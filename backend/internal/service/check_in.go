package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 签到赠送额度（check-in reward）领域模型。
//
// 设计要点：
//   - 奖励直接入账 users.balance（USD），与全局计费体系一致
//   - 每用户每天最多一条签到记录，DB 唯一索引 (user_id, check_in_date) 是幂等真相来源
//   - Redis SETNX 仅作快路径挡板，失败/降级时不影响正确性
//   - 奖励按 7 天周期表发放（rewards[(streak-1) % len(rewards)]），断签重置为 1
//   - 注册冷却（min_reg_age_hours）与月封顶（max_monthly_amount）防薅羊毛

const (
	// CheckInCycleDays 签到奖励周期长度（天）
	CheckInCycleDays = 7
	// checkInDateLayout check_in_date 的字符串格式（自然日）
	checkInDateLayout = "2006-01-02"
)

// 默认签到配置（与 InitializeDefaultSettings 中的默认值保持一致）
var (
	DefaultCheckInRewards          = []float64{0.02, 0.02, 0.03, 0.03, 0.04, 0.05, 0.10}
	DefaultCheckInMinRegAgeHours   = 24
	DefaultCheckInMaxMonthlyAmount = 0.0 // 0 = 不限制
)

// CheckInRecord 签到记录（审计型流水，硬删除）
type CheckInRecord struct {
	ID           int64
	UserID       int64
	CheckInDate  time.Time // 自然日零点（应用全局时区）
	StreakDays   int
	RewardAmount float64
	CreatedAt    time.Time
}

// CheckInRecordRepository 签到记录的持久化接口。
type CheckInRecordRepository interface {
	// GetByDate 查询用户某天的签到记录；不存在返回 ErrCheckInNotFound。
	GetByDate(ctx context.Context, userID int64, date time.Time) (*CheckInRecord, error)
	// ListRecent 返回用户最近 limit 条签到记录（按签到日期倒序）。
	ListRecent(ctx context.Context, userID int64, limit int) ([]CheckInRecord, error)
	// RecordCheckIn 在单个事务内完成：插入签到记录 + 用户余额原子累加 + 支付审计流水。
	// 同日记录已存在（唯一约束冲突）时返回 ErrCheckInAlreadyCheckedIn。
	// 返回签到后的余额。
	RecordCheckIn(ctx context.Context, userID int64, date time.Time, streak int, reward float64) (float64, error)
	// SumRewardsSince 统计用户自 since（含）以来的累计签到奖励金额，用于月封顶。
	SumRewardsSince(ctx context.Context, userID int64, since time.Time) (float64, error)
}

// CheckInDedupCache 签到幂等的 Redis 快路径（非权威，仅挡重复请求）。
type CheckInDedupCache interface {
	// Claim 尝试占用用户当日签到位；已占用返回 false。
	Claim(ctx context.Context, userID int64, dayKey string) (bool, error)
	// Release 释放用户当日签到位（DB 失败时回滚占用）。
	Release(ctx context.Context, userID int64, dayKey string) error
}

var (
	// ErrCheckInDisabled 签到功能未开启
	ErrCheckInDisabled = infraerrors.BadRequest("CHECKIN_DISABLED", "check-in is not enabled")
	// ErrCheckInAlreadyCheckedIn 今天已签到
	ErrCheckInAlreadyCheckedIn = infraerrors.Conflict("CHECKIN_ALREADY_CHECKED_IN", "already checked in today")
	// ErrCheckInRegAgeNotMet 注册时长未达签到门槛
	ErrCheckInRegAgeNotMet = infraerrors.BadRequest("CHECKIN_REG_AGE_NOT_MET", "account is too new to check in")
	// ErrCheckInMonthlyCapReached 本月签到奖励已达封顶
	ErrCheckInMonthlyCapReached = infraerrors.BadRequest("CHECKIN_MONTHLY_CAP_REACHED", "monthly check-in reward cap reached")
	// ErrCheckInInvalidConfig 签到配置非法（奖励表为空/负数等）
	ErrCheckInInvalidConfig = infraerrors.New(500, "CHECKIN_INVALID_CONFIG", "check-in configuration is invalid")
	// ErrCheckInNotFound 签到记录不存在（仓库内部哨兵）
	ErrCheckInNotFound = fmt.Errorf("check-in record not found")
)

// CheckInStatus 签到状态（GET /user/check-in 响应）
type CheckInStatus struct {
	Enabled              bool      `json:"enabled"`
	TodayChecked         bool      `json:"today_checked"`
	CanCheckIn           bool      `json:"can_check_in"`
	StreakDays           int       `json:"streak_days"`             // 已签 = 含今天；未签 = 截至昨天
	TodayReward          float64   `json:"today_reward"`            // 已签 = 实得；未签 = 今天可得
	Rewards              []float64 `json:"rewards"`                 // 完整周期表
	RegAgeRemainingHours int       `json:"reg_age_remaining_hours"` // 未达注册门槛时的剩余小时
	MonthlyCapRemaining  float64   `json:"monthly_cap_remaining"`   // -1 = 不限制
}

// CheckInResult 签到结果（POST /user/check-in 响应）
type CheckInResult struct {
	CheckInDate  string  `json:"check_in_date"`
	Reward       float64 `json:"reward"`
	StreakDays   int     `json:"streak_days"`
	BalanceAfter float64 `json:"balance_after"`
}

// checkInConfig 解析后的签到配置
type checkInConfig struct {
	enabled          bool
	rewards          []float64
	minRegAgeHours   int
	maxMonthlyAmount float64
}

// CheckInService 签到服务
type CheckInService struct {
	records  CheckInRecordRepository
	users    UserRepository
	settings SettingRepository
	cache    CheckInDedupCache
	now      func() time.Time // 测试注入
}

// NewCheckInService 创建签到服务
func NewCheckInService(
	records CheckInRecordRepository,
	users UserRepository,
	settings SettingRepository,
	cache CheckInDedupCache,
) *CheckInService {
	return &CheckInService{
		records:  records,
		users:    users,
		settings: settings,
		cache:    cache,
		now:      time.Now,
	}
}

// loadConfig 读取并解析签到配置；缺失的键回退到默认值。
func (s *CheckInService) loadConfig(ctx context.Context) (checkInConfig, error) {
	values, err := s.settings.GetMultiple(ctx, []string{
		SettingKeyCheckInEnabled,
		SettingKeyCheckInRewards,
		SettingKeyCheckInMinRegAgeHours,
		SettingKeyCheckInMaxMonthlyAmount,
	})
	if err != nil {
		return checkInConfig{}, fmt.Errorf("load check-in settings: %w", err)
	}

	cfg := checkInConfig{
		enabled:          true,
		rewards:          DefaultCheckInRewards,
		minRegAgeHours:   DefaultCheckInMinRegAgeHours,
		maxMonthlyAmount: DefaultCheckInMaxMonthlyAmount,
	}
	if v, ok := values[SettingKeyCheckInEnabled]; ok && v != "" {
		cfg.enabled = v == "true" || v == "1"
	}
	if v, ok := values[SettingKeyCheckInRewards]; ok && v != "" {
		var rewards []float64
		if err := json.Unmarshal([]byte(v), &rewards); err != nil {
			return checkInConfig{}, fmt.Errorf("%w: parse checkin_rewards: %v", ErrCheckInInvalidConfig, err)
		}
		cfg.rewards = rewards
	}
	if v, ok := values[SettingKeyCheckInMinRegAgeHours]; ok && v != "" {
		if _, err := fmt.Sscanf(v, "%d", &cfg.minRegAgeHours); err != nil {
			return checkInConfig{}, fmt.Errorf("%w: parse checkin_min_reg_age_hours: %v", ErrCheckInInvalidConfig, err)
		}
	}
	if v, ok := values[SettingKeyCheckInMaxMonthlyAmount]; ok && v != "" {
		if _, err := fmt.Sscanf(v, "%g", &cfg.maxMonthlyAmount); err != nil {
			return checkInConfig{}, fmt.Errorf("%w: parse checkin_max_monthly_amount: %v", ErrCheckInInvalidConfig, err)
		}
	}

	if len(cfg.rewards) == 0 {
		return checkInConfig{}, fmt.Errorf("%w: rewards table is empty", ErrCheckInInvalidConfig)
	}
	for _, r := range cfg.rewards {
		if r <= 0 {
			return checkInConfig{}, fmt.Errorf("%w: reward must be positive, got %v", ErrCheckInInvalidConfig, r)
		}
	}
	return cfg, nil
}

// rewardFor 按连续天数取周期奖励
func rewardFor(rewards []float64, streak int) float64 {
	if len(rewards) == 0 {
		return 0
	}
	return rewards[(streak-1)%len(rewards)]
}

// dayStart 返回自然日零点（应用全局时区）
func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// GetStatus 返回当前用户的签到状态；功能关闭时返回 Enabled=false（不报错）。
func (s *CheckInService) GetStatus(ctx context.Context, userID int64) (*CheckInStatus, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}

	status := &CheckInStatus{
		Enabled:             cfg.enabled,
		Rewards:             cfg.rewards,
		MonthlyCapRemaining: -1,
	}
	if !cfg.enabled {
		return status, nil
	}

	now := s.now()
	today := dayStart(now)

	// 用户（注册冷却需要 CreatedAt）
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	regAge := now.Sub(user.CreatedAt)
	if cfg.minRegAgeHours > 0 && regAge < time.Duration(cfg.minRegAgeHours)*time.Hour {
		remaining := int(time.Duration(cfg.minRegAgeHours)*time.Hour-regAge) / int(time.Hour)
		status.RegAgeRemainingHours = remaining
		status.CanCheckIn = false
		return status, nil
	}

	// 今日记录
	todayRec, err := s.records.GetByDate(ctx, userID, today)
	if err == nil {
		status.TodayChecked = true
		status.StreakDays = todayRec.StreakDays
		status.TodayReward = todayRec.RewardAmount
		return status, nil
	}
	if !errors.Is(err, ErrCheckInNotFound) {
		return nil, err
	}

	// 未签：streak = 截至昨天的连续天数
	streak := 0
	recent, err := s.records.ListRecent(ctx, userID, 1)
	if err == nil && len(recent) > 0 {
		last := recent[0].CheckInDate.In(now.Location())
		if isYesterday(last, today) {
			streak = recent[0].StreakDays
		}
	} else if err != nil && !errors.Is(err, ErrCheckInNotFound) {
		return nil, err
	}
	status.StreakDays = streak
	status.TodayReward = rewardFor(cfg.rewards, streak+1)

	// 月封顶剩余
	if cfg.maxMonthlyAmount > 0 {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		used, err := s.records.SumRewardsSince(ctx, userID, monthStart)
		if err != nil {
			return nil, err
		}
		remaining := cfg.maxMonthlyAmount - used
		if remaining < 0 {
			remaining = 0
		}
		status.MonthlyCapRemaining = remaining
	}

	status.CanCheckIn = true
	return status, nil
}

// CheckIn 执行签到：发奖入余额。幂等——同一天重复调用返回 ErrCheckInAlreadyCheckedIn。
func (s *CheckInService) CheckIn(ctx context.Context, userID int64) (*CheckInResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.enabled {
		return nil, ErrCheckInDisabled
	}

	now := s.now()
	today := dayStart(now)

	// 用户与注册冷却
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cfg.minRegAgeHours > 0 && now.Sub(user.CreatedAt) < time.Duration(cfg.minRegAgeHours)*time.Hour {
		return nil, ErrCheckInRegAgeNotMet
	}

	// Redis 快路径（失败降级，正确性由 DB 唯一索引兜底）
	dayKey := today.Format(checkInDateLayout)
	claimed, cacheErr := s.cache.Claim(ctx, userID, dayKey)
	if cacheErr != nil {
		claimed = false // 降级：直接走 DB
	}
	if claimed {
		defer func() {
			if err != nil {
				_ = s.cache.Release(ctx, userID, dayKey)
			}
		}()
	}

	// 已签复查（DB 为真相来源）
	if _, err := s.records.GetByDate(ctx, userID, today); err == nil {
		return nil, ErrCheckInAlreadyCheckedIn
	} else if !errors.Is(err, ErrCheckInNotFound) {
		return nil, err
	}

	// 连续天数：昨天签过 → streak+1，否则从 1 开始
	streak := 1
	recent, err := s.records.ListRecent(ctx, userID, 1)
	if err == nil && len(recent) > 0 {
		last := recent[0].CheckInDate.In(now.Location())
		if isYesterday(last, today) {
			streak = recent[0].StreakDays + 1
		}
	} else if err != nil && !errors.Is(err, ErrCheckInNotFound) {
		return nil, err
	}

	reward := rewardFor(cfg.rewards, streak)

	// 月封顶
	if cfg.maxMonthlyAmount > 0 {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		used, err := s.records.SumRewardsSince(ctx, userID, monthStart)
		if err != nil {
			return nil, err
		}
		if used+reward > cfg.maxMonthlyAmount {
			return nil, ErrCheckInMonthlyCapReached
		}
	}

	// 事务：记录 + 加余额 + 审计（唯一冲突 → 已签到）
	balanceAfter, err := s.records.RecordCheckIn(ctx, userID, today, streak, reward)
	if err != nil {
		return nil, err
	}

	return &CheckInResult{
		CheckInDate:  dayKey,
		Reward:       reward,
		StreakDays:   streak,
		BalanceAfter: balanceAfter,
	}, nil
}

// isYesterday 判断 last 是否为 today 的前一天（自然日语义）
func isYesterday(last, today time.Time) bool {
	y, m, d := today.AddDate(0, 0, -1).Date()
	return last.Year() == y && last.Month() == m && last.Day() == d
}
