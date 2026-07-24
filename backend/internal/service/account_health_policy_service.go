package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// AccountHealthPolicyService provides CRUD and execution for group health policies.
type AccountHealthPolicyService struct {
	policyRepo     AccountHealthPolicyRepository
	runRepo        AccountHealthRunRepository
	groupRepo      GroupRepository
	accountRepo    AccountRepository
	settingSvc     *SettingService
	accountTestSvc *AccountTestService
	rateLimitSvc   *RateLimitService

	inFlight sync.Map // policyID -> struct{}
}

func NewAccountHealthPolicyService(
	policyRepo AccountHealthPolicyRepository,
	runRepo AccountHealthRunRepository,
	groupRepo GroupRepository,
	accountRepo AccountRepository,
	settingSvc *SettingService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
) *AccountHealthPolicyService {
	return &AccountHealthPolicyService{
		policyRepo:     policyRepo,
		runRepo:        runRepo,
		groupRepo:      groupRepo,
		accountRepo:    accountRepo,
		settingSvc:     settingSvc,
		accountTestSvc: accountTestSvc,
		rateLimitSvc:   rateLimitSvc,
	}
}

func (s *AccountHealthPolicyService) GetByGroup(ctx context.Context, groupID int64) (*AccountHealthPolicy, error) {
	if groupID <= 0 {
		return nil, errors.New("invalid group id")
	}
	return s.policyRepo.GetByGroupID(ctx, groupID)
}

func (s *AccountHealthPolicyService) Upsert(ctx context.Context, groupID int64, req AccountHealthPolicyUpsert) (*AccountHealthPolicy, error) {
	if groupID <= 0 {
		return nil, errors.New("invalid group id")
	}
	if _, err := s.groupRepo.GetByID(ctx, groupID); err != nil {
		return nil, fmt.Errorf("group not found: %w", err)
	}

	runtime := s.settingSvc.GetAccountHealthPolicyRuntime(ctx)
	existing, err := s.policyRepo.GetByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}

	policy := &AccountHealthPolicy{
		GroupID:                     groupID,
		Enabled:                     false,
		CronExpression:              runtime.DefaultCron,
		ModelID:                     "",
		PreferredModels:             []string{},
		Concurrency:                 runtime.DefaultConcurrency,
		TimeoutSeconds:              runtime.DefaultTimeoutSeconds,
		ConsecutiveFailureThreshold: runtime.DefaultFailureThreshold,
		OnFailureAction:             AccountHealthFailureActionDisableSchedulable,
		AllowDelete:                 false,
		OnSuccessRecover:            true,
		OnSuccessEnableIfDisabled:   true,
		MaxRunHistory:               50,
	}
	if existing != nil {
		*policy = *existing
		policy.AllowDelete = false
	}

	if req.Enabled != nil {
		policy.Enabled = *req.Enabled
	}
	if cron := strings.TrimSpace(req.CronExpression); cron != "" {
		policy.CronExpression = cron
	}
	if model := strings.TrimSpace(req.ModelID); model != "" {
		policy.ModelID = model
	}
	if req.PreferredModels != nil {
		policy.PreferredModels = sanitizePreferredModels(req.PreferredModels)
	}
	if req.Concurrency > 0 {
		policy.Concurrency = clampAccountHealthPolicyConcurrency(req.Concurrency)
	}
	if req.TimeoutSeconds > 0 {
		policy.TimeoutSeconds = clampAccountHealthPolicyTimeout(req.TimeoutSeconds)
	}
	if req.ConsecutiveFailureThreshold > 0 {
		policy.ConsecutiveFailureThreshold = clampAccountHealthPolicyThreshold(req.ConsecutiveFailureThreshold)
	}
	if action := strings.TrimSpace(req.OnFailureAction); action != "" {
		if action != AccountHealthFailureActionNone && action != AccountHealthFailureActionDisableSchedulable {
			return nil, fmt.Errorf("invalid on_failure_action: %s", action)
		}
		policy.OnFailureAction = action
	}
	if req.OnSuccessRecover != nil {
		policy.OnSuccessRecover = *req.OnSuccessRecover
	}
	if req.OnSuccessEnableIfDisabled != nil {
		policy.OnSuccessEnableIfDisabled = *req.OnSuccessEnableIfDisabled
	}
	if req.MaxRunHistory > 0 {
		if req.MaxRunHistory > 500 {
			policy.MaxRunHistory = 500
		} else {
			policy.MaxRunHistory = req.MaxRunHistory
		}
	}

	if err := s.validatePolicy(policy); err != nil {
		return nil, err
	}

	nextRun, err := computeNextRun(policy.CronExpression, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	policy.NextRunAt = &nextRun
	policy.AllowDelete = false

	return s.policyRepo.Upsert(ctx, policy)
}

func (s *AccountHealthPolicyService) DeleteByGroup(ctx context.Context, groupID int64) error {
	if groupID <= 0 {
		return errors.New("invalid group id")
	}
	return s.policyRepo.DeleteByGroupID(ctx, groupID)
}

func (s *AccountHealthPolicyService) ListRuns(ctx context.Context, groupID int64, limit int) ([]*AccountHealthRun, error) {
	if groupID <= 0 {
		return nil, errors.New("invalid group id")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.runRepo.ListRunsByGroupID(ctx, groupID, limit)
}

func (s *AccountHealthPolicyService) GetRun(ctx context.Context, runID int64) (*AccountHealthRun, error) {
	if runID <= 0 {
		return nil, errors.New("invalid run id")
	}
	run, err := s.runRepo.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	items, err := s.runRepo.ListItemsByRunID(ctx, runID)
	if err != nil {
		return nil, err
	}
	run.Items = items
	return run, nil
}

func (s *AccountHealthPolicyService) RunNow(ctx context.Context, groupID int64) (*AccountHealthRun, error) {
	if !s.settingSvc.GetAccountHealthPolicyRuntime(ctx).Enabled {
		return nil, errors.New("account health policy is disabled")
	}
	policy, err := s.policyRepo.GetByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, errors.New("health policy not configured for group")
	}
	return s.ExecutePolicy(ctx, policy, AccountHealthRunTriggerManual)
}

// ExecutePolicy runs a single policy. Used by runner and RunNow.
func (s *AccountHealthPolicyService) ExecutePolicy(ctx context.Context, policy *AccountHealthPolicy, trigger string) (*AccountHealthRun, error) {
	if policy == nil {
		return nil, errors.New("policy is nil")
	}
	if _, loaded := s.inFlight.LoadOrStore(policy.ID, struct{}{}); loaded {
		return nil, errors.New("policy run already in progress")
	}
	defer s.inFlight.Delete(policy.ID)

	startedAt := time.Now()
	run, err := s.runRepo.CreateRun(ctx, &AccountHealthRun{
		PolicyID:  policy.ID,
		GroupID:   policy.GroupID,
		Trigger:   trigger,
		Status:    AccountHealthRunStatusRunning,
		StartedAt: startedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	accounts, err := s.accountRepo.ListByGroup(ctx, policy.GroupID)
	if err != nil {
		s.finishFailedRun(ctx, run, fmt.Sprintf("list accounts: %v", err), startedAt)
		return run, err
	}

	modelID := resolveAccountHealthModel(policy)
	if modelID == "" {
		s.finishFailedRun(ctx, run, "model_id is required", startedAt)
		return run, errors.New("model_id is required")
	}

	concurrency := clampAccountHealthPolicyConcurrency(policy.Concurrency)
	timeout := time.Duration(clampAccountHealthPolicyTimeout(policy.TimeoutSeconds)) * time.Second

	jobs := make(chan Account, len(accounts))
	for _, acc := range accounts {
		jobs <- acc
	}
	close(jobs)

	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		successCount int64
		failureCount int64
		actionCount  int64
		items        []*AccountHealthRunItem
	)

	workerCount := concurrency
	if workerCount > len(accounts) {
		workerCount = len(accounts)
	}
	if workerCount < 1 && len(accounts) > 0 {
		workerCount = 1
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for acc := range jobs {
				if ctx.Err() != nil {
					return
				}
				item := s.probeAccount(ctx, policy, run.ID, acc, modelID, timeout)
				mu.Lock()
				items = append(items, item)
				mu.Unlock()
				switch item.Status {
				case AccountHealthItemStatusSuccess:
					atomic.AddInt64(&successCount, 1)
				case AccountHealthItemStatusFailed:
					atomic.AddInt64(&failureCount, 1)
				}
				if item.ActionTaken != AccountHealthActionNone {
					atomic.AddInt64(&actionCount, 1)
				}
				if _, err := s.runRepo.CreateItem(ctx, item); err != nil {
					logger.LegacyPrintf("service.account_health_policy", "[AccountHealthPolicy] create item account=%d: %v", acc.ID, err)
				}
			}
		}()
	}
	wg.Wait()

	finishedAt := time.Now()
	run.TotalCount = len(accounts)
	run.SuccessCount = int(successCount)
	run.FailureCount = int(failureCount)
	run.ActionCount = int(actionCount)
	run.FinishedAt = &finishedAt
	run.Status = summarizeRunStatus(run.TotalCount, run.SuccessCount, run.FailureCount)
	run.Items = items

	if err := s.runRepo.FinishRun(ctx, run); err != nil {
		logger.LegacyPrintf("service.account_health_policy", "[AccountHealthPolicy] finish run=%d: %v", run.ID, err)
	}

	nextRun, err := computeNextRun(policy.CronExpression, time.Now())
	if err != nil {
		logger.LegacyPrintf("service.account_health_policy", "[AccountHealthPolicy] policy=%d next run: %v", policy.ID, err)
	} else if err := s.policyRepo.UpdateAfterRun(ctx, policy.ID, finishedAt, nextRun); err != nil {
		logger.LegacyPrintf("service.account_health_policy", "[AccountHealthPolicy] policy=%d UpdateAfterRun: %v", policy.ID, err)
	}

	if err := s.runRepo.PruneOldRuns(ctx, policy.ID, policy.MaxRunHistory); err != nil {
		logger.LegacyPrintf("service.account_health_policy", "[AccountHealthPolicy] policy=%d prune: %v", policy.ID, err)
	}

	return run, nil
}

func (s *AccountHealthPolicyService) probeAccount(
	parent context.Context,
	policy *AccountHealthPolicy,
	runID int64,
	account Account,
	modelID string,
	timeout time.Duration,
) *AccountHealthRunItem {
	startedAt := time.Now()
	item := &AccountHealthRunItem{
		RunID:       runID,
		AccountID:   account.ID,
		AccountName: account.Name,
		ModelID:     modelID,
		ActionTaken: AccountHealthActionNone,
		StartedAt:   startedAt,
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	testResult, err := s.accountTestSvc.RunTestBackground(ctx, account.ID, modelID)
	finishedAt := time.Now()
	item.FinishedAt = finishedAt
	if testResult != nil {
		item.LatencyMs = testResult.LatencyMs
		item.ResponseExcerpt = truncateRunText(testResult.ResponseText, 2048)
		if testResult.ErrorMessage != "" {
			item.ErrorMessage = testResult.ErrorMessage
		}
	}
	if err != nil && item.ErrorMessage == "" {
		item.ErrorMessage = err.Error()
	}

	success := err == nil && testResult != nil && testResult.Status == "success"
	if success {
		item.Status = AccountHealthItemStatusSuccess
	} else {
		item.Status = AccountHealthItemStatusFailed
		if item.ErrorMessage == "" {
			item.ErrorMessage = "probe failed"
		}
	}

	action, consecutive, applyErr := s.applyResult(parent, policy, &account, success)
	if applyErr != nil {
		logger.LegacyPrintf("service.account_health_policy", "[AccountHealthPolicy] applyResult account=%d: %v", account.ID, applyErr)
		if item.ErrorMessage == "" {
			item.ErrorMessage = applyErr.Error()
		}
	}
	item.ActionTaken = action
	item.ConsecutiveFailures = consecutive
	return item
}

// applyResult updates consecutive failure counters and applies success/failure actions.
// Exported behavior is covered by unit tests via this method.
func (s *AccountHealthPolicyService) applyResult(
	ctx context.Context,
	policy *AccountHealthPolicy,
	account *Account,
	success bool,
) (actionTaken string, consecutive int, err error) {
	if account == nil || policy == nil {
		return AccountHealthActionNone, 0, errors.New("nil policy or account")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if success {
		consecutive = 0
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			AccountHealthConsecutiveFailuresKey: 0,
			AccountHealthLastSuccessAtKey:       now,
		}); err != nil {
			return AccountHealthActionNone, 0, err
		}

		recovered := false
		if policy.OnSuccessRecover && s.rateLimitSvc != nil {
			if _, recErr := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, account.ID); recErr != nil {
				logger.LegacyPrintf("service.account_health_policy", "[AccountHealthPolicy] recover account=%d: %v", account.ID, recErr)
			} else {
				recovered = true
			}
		}

		enabled := false
		if policy.OnSuccessEnableIfDisabled && !account.Schedulable {
			if setErr := s.accountRepo.SetSchedulable(ctx, account.ID, true); setErr != nil {
				return AccountHealthActionNone, 0, setErr
			}
			enabled = true
			account.Schedulable = true
		}

		switch {
		case recovered && enabled:
			return AccountHealthActionRecoverAndEnable, 0, nil
		case recovered:
			return AccountHealthActionRecover, 0, nil
		case enabled:
			return AccountHealthActionEnableSchedulable, 0, nil
		default:
			return AccountHealthActionNone, 0, nil
		}
	}

	prev := readHealthConsecutiveFailures(account.Extra)
	consecutive = prev + 1
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		AccountHealthConsecutiveFailuresKey: consecutive,
		AccountHealthLastFailureAtKey:       now,
	}); err != nil {
		return AccountHealthActionNone, consecutive, err
	}

	if policy.OnFailureAction == AccountHealthFailureActionDisableSchedulable &&
		consecutive >= policy.ConsecutiveFailureThreshold &&
		account.Schedulable {
		if setErr := s.accountRepo.SetSchedulable(ctx, account.ID, false); setErr != nil {
			return AccountHealthActionNone, consecutive, setErr
		}
		account.Schedulable = false
		return AccountHealthActionDisableSchedulable, consecutive, nil
	}

	return AccountHealthActionNone, consecutive, nil
}

func (s *AccountHealthPolicyService) validatePolicy(policy *AccountHealthPolicy) error {
	if strings.TrimSpace(policy.CronExpression) == "" {
		return errors.New("cron_expression is required")
	}
	if _, err := computeNextRun(policy.CronExpression, time.Now()); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	if resolveAccountHealthModel(policy) == "" {
		return errors.New("model_id is required")
	}
	if policy.OnFailureAction != AccountHealthFailureActionNone &&
		policy.OnFailureAction != AccountHealthFailureActionDisableSchedulable {
		return fmt.Errorf("invalid on_failure_action: %s", policy.OnFailureAction)
	}
	return nil
}

func (s *AccountHealthPolicyService) finishFailedRun(ctx context.Context, run *AccountHealthRun, msg string, startedAt time.Time) {
	finishedAt := time.Now()
	run.Status = AccountHealthRunStatusFailed
	run.ErrorMessage = msg
	run.FinishedAt = &finishedAt
	_ = s.runRepo.FinishRun(ctx, run)
	_ = startedAt
}

func resolveAccountHealthModel(policy *AccountHealthPolicy) string {
	if policy == nil {
		return ""
	}
	if model := strings.TrimSpace(policy.ModelID); model != "" {
		return model
	}
	for _, m := range policy.PreferredModels {
		if model := strings.TrimSpace(m); model != "" {
			return model
		}
	}
	return ""
}

func sanitizePreferredModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func summarizeRunStatus(total, success, failure int) string {
	if total == 0 {
		return AccountHealthRunStatusSuccess
	}
	if failure == 0 {
		return AccountHealthRunStatusSuccess
	}
	if success == 0 {
		return AccountHealthRunStatusFailed
	}
	return AccountHealthRunStatusPartial
}

func truncateRunText(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max]
}

func readHealthConsecutiveFailures(extra map[string]any) int {
	if extra == nil {
		return 0
	}
	raw, ok := extra[AccountHealthConsecutiveFailuresKey]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case int:
		if v < 0 {
			return 0
		}
		return v
	case int64:
		if v < 0 {
			return 0
		}
		return int(v)
	case float64:
		if v < 0 {
			return 0
		}
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil || n < 0 {
			return 0
		}
		return int(n)
	default:
		return 0
	}
}
