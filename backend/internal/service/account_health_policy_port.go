package service

import (
	"context"
	"time"
)

// Account health consecutive-failure counters live in accounts.extra.
const (
	AccountHealthConsecutiveFailuresKey = "health_consecutive_failures"
	AccountHealthLastFailureAtKey       = "health_last_failure_at"
	AccountHealthLastSuccessAtKey       = "health_last_success_at"
)

// Failure / success action constants.
const (
	AccountHealthFailureActionNone               = "none"
	AccountHealthFailureActionDisableSchedulable = "disable_schedulable"

	AccountHealthActionNone              = "none"
	AccountHealthActionDisableSchedulable = "disable_schedulable"
	AccountHealthActionEnableSchedulable  = "enable_schedulable"
	AccountHealthActionRecover            = "recover"
	AccountHealthActionRecoverAndEnable   = "recover_and_enable"

	AccountHealthRunTriggerCron   = "cron"
	AccountHealthRunTriggerManual = "manual"

	AccountHealthRunStatusRunning = "running"
	AccountHealthRunStatusSuccess = "success"
	AccountHealthRunStatusPartial = "partial"
	AccountHealthRunStatusFailed  = "failed"

	AccountHealthItemStatusSuccess = "success"
	AccountHealthItemStatusFailed  = "failed"
	AccountHealthItemStatusSkipped = "skipped"
)

// AccountHealthPolicy is a group-scoped health-check policy.
type AccountHealthPolicy struct {
	ID                          int64      `json:"id"`
	GroupID                     int64      `json:"group_id"`
	Enabled                     bool       `json:"enabled"`
	CronExpression              string     `json:"cron_expression"`
	ModelID                     string     `json:"model_id"`
	PreferredModels             []string   `json:"preferred_models"`
	Concurrency                 int        `json:"concurrency"`
	TimeoutSeconds              int        `json:"timeout_seconds"`
	ConsecutiveFailureThreshold int        `json:"consecutive_failure_threshold"`
	OnFailureAction             string     `json:"on_failure_action"`
	AllowDelete                 bool       `json:"allow_delete"`
	OnSuccessRecover            bool       `json:"on_success_recover"`
	OnSuccessEnableIfDisabled   bool       `json:"on_success_enable_if_disabled"`
	MaxRunHistory               int        `json:"max_run_history"`
	LastRunAt                   *time.Time `json:"last_run_at"`
	NextRunAt                   *time.Time `json:"next_run_at"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

// AccountHealthRun is one policy execution.
type AccountHealthRun struct {
	ID           int64      `json:"id"`
	PolicyID     int64      `json:"policy_id"`
	GroupID      int64      `json:"group_id"`
	Trigger      string     `json:"trigger"`
	Status       string     `json:"status"`
	TotalCount   int        `json:"total_count"`
	SuccessCount int        `json:"success_count"`
	FailureCount int        `json:"failure_count"`
	SkippedCount int        `json:"skipped_count"`
	ActionCount  int        `json:"action_count"`
	ErrorMessage string     `json:"error_message"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	CreatedAt    time.Time  `json:"created_at"`
	Items        []*AccountHealthRunItem `json:"items,omitempty"`
}

// AccountHealthRunItem is one account outcome inside a run.
type AccountHealthRunItem struct {
	ID                   int64     `json:"id"`
	RunID                int64     `json:"run_id"`
	AccountID            int64     `json:"account_id"`
	AccountName          string    `json:"account_name"`
	ModelID              string    `json:"model_id"`
	Status               string    `json:"status"`
	LatencyMs            int64     `json:"latency_ms"`
	ErrorMessage         string    `json:"error_message"`
	ConsecutiveFailures  int       `json:"consecutive_failures"`
	ActionTaken          string    `json:"action_taken"`
	ResponseExcerpt      string    `json:"response_excerpt"`
	StartedAt            time.Time `json:"started_at"`
	FinishedAt           time.Time `json:"finished_at"`
	CreatedAt            time.Time `json:"created_at"`
}

// AccountHealthPolicyUpsert holds fields accepted by PUT.
type AccountHealthPolicyUpsert struct {
	Enabled                     *bool    `json:"enabled"`
	CronExpression              string   `json:"cron_expression"`
	ModelID                     string   `json:"model_id"`
	PreferredModels             []string `json:"preferred_models"`
	Concurrency                 int      `json:"concurrency"`
	TimeoutSeconds              int      `json:"timeout_seconds"`
	ConsecutiveFailureThreshold int      `json:"consecutive_failure_threshold"`
	OnFailureAction             string   `json:"on_failure_action"`
	OnSuccessRecover            *bool    `json:"on_success_recover"`
	OnSuccessEnableIfDisabled   *bool    `json:"on_success_enable_if_disabled"`
	MaxRunHistory               int      `json:"max_run_history"`
}

// AccountHealthPolicyRepository stores group health policies.
type AccountHealthPolicyRepository interface {
	GetByID(ctx context.Context, id int64) (*AccountHealthPolicy, error)
	GetByGroupID(ctx context.Context, groupID int64) (*AccountHealthPolicy, error)
	List(ctx context.Context) ([]*AccountHealthPolicy, error)
	ListDue(ctx context.Context, now time.Time) ([]*AccountHealthPolicy, error)
	Upsert(ctx context.Context, policy *AccountHealthPolicy) (*AccountHealthPolicy, error)
	DeleteByGroupID(ctx context.Context, groupID int64) error
	UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error
}

// AccountHealthRunRepository stores run history.
type AccountHealthRunRepository interface {
	CreateRun(ctx context.Context, run *AccountHealthRun) (*AccountHealthRun, error)
	FinishRun(ctx context.Context, run *AccountHealthRun) error
	CreateItem(ctx context.Context, item *AccountHealthRunItem) (*AccountHealthRunItem, error)
	ListRunsByPolicyID(ctx context.Context, policyID int64, limit int) ([]*AccountHealthRun, error)
	ListRunsByGroupID(ctx context.Context, groupID int64, limit int) ([]*AccountHealthRun, error)
	GetRun(ctx context.Context, id int64) (*AccountHealthRun, error)
	ListItemsByRunID(ctx context.Context, runID int64) ([]*AccountHealthRunItem, error)
	PruneOldRuns(ctx context.Context, policyID int64, keepCount int) error
}
