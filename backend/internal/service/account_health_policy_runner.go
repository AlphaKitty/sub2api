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
	accountHealthPolicyRunnerLeaderTTL = 4 * time.Minute
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

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, accountHealthPolicyRunnerLeaderKey, s.instanceID, accountHealthPolicyRunnerLeaderTTL)
	if !ok {
		return
	}
	defer release()

	if s.settingSvc == nil || !s.settingSvc.GetAccountHealthPolicyRuntime(ctx).Enabled {
		return
	}

	now := time.Now()
	policies, err := s.policyRepo.ListDue(ctx, now)
	if err != nil {
		logger.LegacyPrintf("service.account_health_policy_runner", "[AccountHealthPolicyRunner] ListDue: %v", err)
		return
	}
	if len(policies) == 0 {
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
