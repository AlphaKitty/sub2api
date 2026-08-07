//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---- fake repositories ----

type fakeCheckInRecords struct {
	mu         sync.Mutex
	records    map[int64][]CheckInRecord // userID → 按日期升序
	created    []CheckInRecord
	recordErr  error // RecordCheckIn 注入错误
	sumOverride map[time.Time]float64
	lastSumSince time.Time
}

func newFakeCheckInRecords() *fakeCheckInRecords {
	return &fakeCheckInRecords{records: map[int64][]CheckInRecord{}}
}

func (f *fakeCheckInRecords) add(userID int64, date time.Time, streak int, reward float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[userID] = append(f.records[userID], CheckInRecord{
		ID: int64(len(f.records[userID]) + 1), UserID: userID,
		CheckInDate: date, StreakDays: streak, RewardAmount: reward,
	})
}

func (f *fakeCheckInRecords) GetByDate(_ context.Context, userID int64, date time.Time) (*CheckInRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.records[userID] {
		r := f.records[userID][i]
		if r.CheckInDate.Equal(date) {
			cloned := r
			return &cloned, nil
		}
	}
	return nil, ErrCheckInNotFound
}

func (f *fakeCheckInRecords) ListRecent(_ context.Context, userID int64, limit int) ([]CheckInRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	recs := f.records[userID]
	if len(recs) == 0 {
		return nil, ErrCheckInNotFound
	}
	out := make([]CheckInRecord, 0, limit)
	for i := len(recs) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, recs[i])
	}
	return out, nil
}

func (f *fakeCheckInRecords) RecordCheckIn(_ context.Context, userID int64, date time.Time, streak int, reward float64) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return 0, f.recordErr
	}
	// 幂等：同日已存在 → 唯一冲突语义
	for i := range f.records[userID] {
		if f.records[userID][i].CheckInDate.Equal(date) {
			return 0, ErrCheckInAlreadyCheckedIn
		}
	}
	f.records[userID] = append(f.records[userID], CheckInRecord{
		ID: int64(len(f.records[userID]) + 1), UserID: userID,
		CheckInDate: date, StreakDays: streak, RewardAmount: reward,
	})
	f.created = append(f.created, CheckInRecord{UserID: userID, CheckInDate: date, StreakDays: streak, RewardAmount: reward})
	return 100 + reward, nil // 模拟余额
}

func (f *fakeCheckInRecords) SumRewardsSince(_ context.Context, userID int64, since time.Time) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSumSince = since
	if v, ok := f.sumOverride[since]; ok {
		return v, nil
	}
	var total float64
	for i := range f.records[userID] {
		if !f.records[userID][i].CheckInDate.Before(since) {
			total += f.records[userID][i].RewardAmount
		}
	}
	return total, nil
}

type fakeCheckInCache struct {
	mu       sync.Mutex
	claimed  map[string]bool
	released []string
	fail     bool
}

func newFakeCheckInCache() *fakeCheckInCache {
	return &fakeCheckInCache{claimed: map[string]bool{}}
}

func (f *fakeCheckInCache) Claim(_ context.Context, userID int64, dayKey string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return false, errors.New("redis down")
	}
	key := fmt.Sprintf("%d:%s", userID, dayKey)
	if f.claimed[key] {
		return false, nil
	}
	f.claimed[key] = true
	return true, nil
}

func (f *fakeCheckInCache) Release(_ context.Context, userID int64, dayKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%d:%s", userID, dayKey)
	delete(f.claimed, key)
	f.released = append(f.released, key)
	return nil
}

type fakeCheckInSettings struct {
	values map[string]string
}

func (f *fakeCheckInSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := f.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// stubUserRepo 嵌入完整 UserRepository 接口，仅覆盖签到用到的 GetByID。
type stubUserRepo struct {
	UserRepository
	user *User
}

func (s *stubUserRepo) GetByID(_ context.Context, _ int64) (*User, error) {
	if s.user == nil {
		return nil, ErrUserNotFound
	}
	cloned := *s.user
	return &cloned, nil
}

// stubCheckInSettings 嵌入完整 SettingRepository 接口（nil），仅覆盖 GetMultiple。
type stubCheckInSettings struct {
	SettingRepository
	values map[string]string
}

func (s *stubCheckInSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := s.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// ---- helpers ----

func newCheckInServiceForTest(now time.Time, records *fakeCheckInRecords, cache *fakeCheckInCache, settings *fakeCheckInSettings, user *User) *CheckInService {
	svc := NewCheckInService(records, &stubUserRepo{user: user}, &stubCheckInSettings{values: settings.values}, cache)
	svc.now = func() time.Time { return now }
	return svc
}

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	loc := time.Local
	d, err := time.ParseInLocation("2006-01-02", s, loc)
	require.NoError(t, err)
	return d
}

// ---- tests ----

func TestCheckIn_FirstTime(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	res, err := svc.CheckIn(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "2025-06-10", res.CheckInDate)
	require.Equal(t, 1, res.StreakDays)
	require.InDelta(t, 0.02, res.Reward, 1e-9) // rewards[0]
	require.InDelta(t, 100.02, res.BalanceAfter, 1e-9)
}

func TestCheckIn_ConsecutiveStreak(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	// 昨天 streak=3
	records.add(7, mustParseDate(t, "2025-06-09"), 3, 0.03)

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	res, err := svc.CheckIn(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 4, res.StreakDays)
	require.InDelta(t, 0.03, res.Reward, 1e-9) // rewards[3]
}

func TestCheckIn_BrokenStreakRestarts(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	// 前天 streak=5，昨天断签
	records.add(7, mustParseDate(t, "2025-06-08"), 5, 0.04)

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	res, err := svc.CheckIn(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, res.StreakDays)
	require.InDelta(t, 0.02, res.Reward, 1e-9)
}

func TestCheckIn_WeekCycleRollover(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	// 连续 7 天 → 今天第 8 天回到周期起点
	for i := 7; i >= 1; i-- {
		records.add(7, mustParseDate(t, "2025-06-10").AddDate(0, 0, -i), 7-i+1, 0.02)
	}

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	res, err := svc.CheckIn(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 8, res.StreakDays)
	require.InDelta(t, 0.02, res.Reward, 1e-9) // rewards[7%7=0]
}

func TestCheckIn_AlreadyCheckedIn(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	records.add(7, mustParseDate(t, "2025-06-10"), 2, 0.02)

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	_, err := svc.CheckIn(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInAlreadyCheckedIn)
}

func TestCheckIn_Disabled(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	svc := newCheckInServiceForTest(now, newFakeCheckInRecords(), newFakeCheckInCache(),
		&fakeCheckInSettings{values: map[string]string{SettingKeyCheckInEnabled: "false"}},
		&User{CreatedAt: now.AddDate(0, -1, 0)})

	_, err := svc.CheckIn(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInDisabled)
}

func TestCheckIn_RegAgeNotMet(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	svc := newCheckInServiceForTest(now, newFakeCheckInRecords(), newFakeCheckInCache(),
		&fakeCheckInSettings{},
		&User{CreatedAt: now.Add(-2 * time.Hour)}) // 注册仅 2 小时

	_, err := svc.CheckIn(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInRegAgeNotMet)
}

func TestCheckIn_MonthlyCapReached(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	records.sumOverride = map[time.Time]float64{mustParseDate(t, "2025-06-01"): 0.99}

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(),
		&fakeCheckInSettings{values: map[string]string{SettingKeyCheckInMaxMonthlyAmount: "1.00"}},
		&User{CreatedAt: now.AddDate(0, -1, 0)})

	_, err := svc.CheckIn(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInMonthlyCapReached)
}

func TestCheckIn_MonthlyCapWithinLimit(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	records.sumOverride = map[time.Time]float64{mustParseDate(t, "2025-06-01"): 0.10}

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(),
		&fakeCheckInSettings{values: map[string]string{SettingKeyCheckInMaxMonthlyAmount: "1.00"}},
		&User{CreatedAt: now.AddDate(0, -1, 0)})

	res, err := svc.CheckIn(context.Background(), 7)
	require.NoError(t, err)
	require.InDelta(t, 0.02, res.Reward, 1e-9)
}

func TestCheckIn_RedisClaimReleasedOnDBFailure(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	records.recordErr = errors.New("db exploded")

	cache := newFakeCheckInCache()
	svc := newCheckInServiceForTest(now, records, cache, &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	_, err := svc.CheckIn(context.Background(), 7)
	require.Error(t, err)
	require.Len(t, cache.released, 1, "claim must be released when DB write fails")
}

func TestCheckIn_RedisDownDegradesGracefully(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	cache := newFakeCheckInCache()
	cache.fail = true

	svc := newCheckInServiceForTest(now, records, cache, &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	res, err := svc.CheckIn(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, res.StreakDays)
}

func TestCheckIn_DoubleSubmitOnlyOneSucceeds(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	// 模拟：第一次成功落库后，第二次请求（即使 Redis claim 失败）也应命中"已签到"
	_, err := svc.CheckIn(context.Background(), 7)
	require.NoError(t, err)

	svc.cache = newFakeCheckInCache() // 新请求，Redis 无占用
	_, err = svc.CheckIn(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInAlreadyCheckedIn)
}

// ---- GetStatus ----

func TestCheckInStatus_NotCheckedIn(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	records.add(7, mustParseDate(t, "2025-06-09"), 3, 0.03) // 昨天 streak=3

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	status, err := svc.GetStatus(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.False(t, status.TodayChecked)
	require.True(t, status.CanCheckIn)
	require.Equal(t, 3, status.StreakDays)        // 截至昨天
	require.InDelta(t, 0.03, status.TodayReward, 1e-9) // 今天将得 rewards[3]
	require.Equal(t, DefaultCheckInRewards, status.Rewards)
}

func TestCheckInStatus_CheckedInToday(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	records.add(7, mustParseDate(t, "2025-06-10"), 4, 0.03)

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(), &fakeCheckInSettings{}, &User{CreatedAt: now.AddDate(0, -1, 0)})

	status, err := svc.GetStatus(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, status.TodayChecked)
	require.False(t, status.CanCheckIn)
	require.Equal(t, 4, status.StreakDays)
	require.InDelta(t, 0.03, status.TodayReward, 1e-9)
}

func TestCheckInStatus_DisabledStillReturnsStatus(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	svc := newCheckInServiceForTest(now, newFakeCheckInRecords(), newFakeCheckInCache(),
		&fakeCheckInSettings{values: map[string]string{SettingKeyCheckInEnabled: "false"}},
		&User{CreatedAt: now.AddDate(0, -1, 0)})

	status, err := svc.GetStatus(context.Background(), 7)
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.False(t, status.CanCheckIn)
}

func TestCheckInStatus_RegAgeRemaining(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	svc := newCheckInServiceForTest(now, newFakeCheckInRecords(), newFakeCheckInCache(),
		&fakeCheckInSettings{},
		&User{CreatedAt: now.Add(-10 * time.Hour)}) // 注册 10 小时，门槛 24h

	status, err := svc.GetStatus(context.Background(), 7)
	require.NoError(t, err)
	require.False(t, status.CanCheckIn)
	require.Equal(t, 14, status.RegAgeRemainingHours)
}

func TestCheckInStatus_MonthlyCapRemaining(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	records := newFakeCheckInRecords()
	records.sumOverride = map[time.Time]float64{mustParseDate(t, "2025-06-01"): 0.30}

	svc := newCheckInServiceForTest(now, records, newFakeCheckInCache(),
		&fakeCheckInSettings{values: map[string]string{SettingKeyCheckInMaxMonthlyAmount: "1.00"}},
		&User{CreatedAt: now.AddDate(0, -1, 0)})

	status, err := svc.GetStatus(context.Background(), 7)
	require.NoError(t, err)
	require.InDelta(t, 0.70, status.MonthlyCapRemaining, 1e-9)
	require.True(t, status.CanCheckIn)
}

func TestCheckIn_InvalidRewardsConfig(t *testing.T) {
	now := mustParseDate(t, "2025-06-10").Add(10 * time.Hour)
	svc := newCheckInServiceForTest(now, newFakeCheckInRecords(), newFakeCheckInCache(),
		&fakeCheckInSettings{values: map[string]string{SettingKeyCheckInRewards: "[]"}},
		&User{CreatedAt: now.AddDate(0, -1, 0)})

	_, err := svc.CheckIn(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInInvalidConfig)
}
