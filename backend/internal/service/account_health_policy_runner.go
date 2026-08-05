package service

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const (
	accountHealthPolicyRunnerLeaderKey = "account_health_policy:runner:leader"
	// 单轮巡检预算：需要覆盖“账号数 × 单账号探测超时 / 并发”的最坏情况。
	// 默认并发 3、超时 60s 时，4 分钟预算只能覆盖约 12 个账号，账号多的分组会被截断、
	// 排在后半段的账号永远探测不到，也不会被自动停用。
	accountHealthPolicyRunnerTickTimeout = 15 * time.Minute
	accountHealthPolicyRunnerLeaderTTL   = 15 * time.Minute
)

// AccountHealthPolicyRunnerService periodically executes due group health policies.
type AccountHealthPolicyRunnerService struct {
	policyRepo AccountHealthPolicyRepository
	healthSvc  *AccountHealthPolicyService
	settingSvc *SettingService
	cfg        *config.Config

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once

	switchOffLogged     sync.Once
	noDuePoliciesLogged sync.Once
}

func NewAccountHealthPolicyRunnerService(
	policyRepo AccountHealthPolicyRepository,
	healthSvc *AccountHealthPolicyService,
	settingSvc *SettingService,
	cfg *config.Config,
) *AccountHealthPolicyRunnerService {
	return &AccountHealthPolicyRunnerService{
		policyRepo: policyRepo,
		healthSvc:  healthSvc,
		settingSvc: settingSvc,
		cfg:        cfg,
		instanceID: "account-health-policy-runner",
	}
}

// SetLeaderLock injects multi-instance coordination backends.
func (s *AccountHealthPolicyRunnerService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB, instanceID string) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
	if id := strings.TrimSpace(instanceID); id != "" {
		s.instanceID = id
	}
}

// Start begins the minute ticker.
func (s *AccountHealthPolicyRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		loc := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				loc = parsed
			}
		}

		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc))
		if _, err := c.AddFunc("* * * * *", func() { s.runScheduled() }); err != nil {
			logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] not started: %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] started (tick=every minute)")
	})
}

// Stop gracefully shuts down the cron scheduler.
func (s *AccountHealthPolicyRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] cron stop timed out")
			}
		}
	})
}

func (s *AccountHealthPolicyRunnerService) runScheduled() {
	// Skew away from :00 like scheduled tests.
	time.Sleep(15 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), accountHealthPolicyRunnerTickTimeout)
	defer cancel()

	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, accountHealthPolicyRunnerLeaderKey, s.instanceID, accountHealthPolicyRunnerLeaderTTL)
	if !ok {
		return
	}
	defer release()

	if s.settingSvc == nil {
		return
	}
	runtime := s.settingSvc.GetAccountHealthPolicyRuntime(ctx)
	if !runtime.Enabled {
		// 只提示一次，避免每分钟刷日志；帮助定位“开了没生效”的配置问题。
		s.switchOffLogged.Do(func() {
			logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] skipped: account_health_policy_enabled is off; enable it in 系统设置 → 功能开关, then enable a per-group health policy in 分组管理 to activate")
		})
		return
	}

	now := time.Now()
	policies, err := s.policyRepo.ListDue(ctx, now)
	if err != nil {
		logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] ListDue: %v", err)
		return
	}
	if len(policies) == 0 {
		// 开关已开启但没有到期策略：一次性提示，避免每分钟刷屏。
		// 常见原因：分组策略未启用（account_health_policies.enabled=false）、
		// next_run_at 在未来、或该分组没有策略。
		s.noDuePoliciesLogged.Do(func() {
			logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] no due policies: open 分组管理 → 对应分组 → 健康巡检 and make sure the policy is enabled with a model and a valid cron (next_run_at must be in the past)")
		})
		return
	}

	logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] found %d due policies", len(policies))
	for _, policy := range policies {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.healthSvc.ExecutePolicy(ctx, policy, AccountHealthRunTriggerCron); err != nil {
			logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] policy=%d group=%d: %v", policy.ID, policy.GroupID, err)
		}
	}
}
