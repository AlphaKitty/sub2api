package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountHealthPolicyRepository struct {
	db *sql.DB
}

func NewAccountHealthPolicyRepository(db *sql.DB) service.AccountHealthPolicyRepository {
	return &accountHealthPolicyRepository{db: db}
}

const accountHealthPolicySelectCols = `
	id, group_id, enabled, cron_expression, model_id, preferred_models,
	concurrency, timeout_seconds, consecutive_failure_threshold, on_failure_action,
	allow_delete, on_success_recover, on_success_enable_if_disabled, max_run_history,
	last_run_at, next_run_at, created_at, updated_at
`

func (r *accountHealthPolicyRepository) GetByID(ctx context.Context, id int64) (*service.AccountHealthPolicy, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+accountHealthPolicySelectCols+`
		FROM account_health_policies WHERE id = $1
	`, id)
	return scanAccountHealthPolicy(row)
}

func (r *accountHealthPolicyRepository) GetByGroupID(ctx context.Context, groupID int64) (*service.AccountHealthPolicy, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+accountHealthPolicySelectCols+`
		FROM account_health_policies WHERE group_id = $1
	`, groupID)
	p, err := scanAccountHealthPolicy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *accountHealthPolicyRepository) List(ctx context.Context) ([]*service.AccountHealthPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+accountHealthPolicySelectCols+`
		FROM account_health_policies
		ORDER BY group_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanAccountHealthPolicies(rows)
}

func (r *accountHealthPolicyRepository) ListDue(ctx context.Context, now time.Time) ([]*service.AccountHealthPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+accountHealthPolicySelectCols+`
		FROM account_health_policies
		WHERE enabled = true AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanAccountHealthPolicies(rows)
}

func (r *accountHealthPolicyRepository) Upsert(ctx context.Context, policy *service.AccountHealthPolicy) (*service.AccountHealthPolicy, error) {
	preferred, err := json.Marshal(policy.PreferredModels)
	if err != nil {
		return nil, fmt.Errorf("marshal preferred_models: %w", err)
	}
	if preferred == nil {
		preferred = []byte("[]")
	}

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO account_health_policies (
			group_id, enabled, cron_expression, model_id, preferred_models,
			concurrency, timeout_seconds, consecutive_failure_threshold, on_failure_action,
			allow_delete, on_success_recover, on_success_enable_if_disabled, max_run_history,
			next_run_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5::jsonb,
			$6, $7, $8, $9,
			$10, $11, $12, $13,
			$14, NOW(), NOW()
		)
		ON CONFLICT (group_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			cron_expression = EXCLUDED.cron_expression,
			model_id = EXCLUDED.model_id,
			preferred_models = EXCLUDED.preferred_models,
			concurrency = EXCLUDED.concurrency,
			timeout_seconds = EXCLUDED.timeout_seconds,
			consecutive_failure_threshold = EXCLUDED.consecutive_failure_threshold,
			on_failure_action = EXCLUDED.on_failure_action,
			allow_delete = EXCLUDED.allow_delete,
			on_success_recover = EXCLUDED.on_success_recover,
			on_success_enable_if_disabled = EXCLUDED.on_success_enable_if_disabled,
			max_run_history = EXCLUDED.max_run_history,
			next_run_at = EXCLUDED.next_run_at,
			updated_at = NOW()
		RETURNING `+accountHealthPolicySelectCols+`
	`,
		policy.GroupID, policy.Enabled, policy.CronExpression, policy.ModelID, string(preferred),
		policy.Concurrency, policy.TimeoutSeconds, policy.ConsecutiveFailureThreshold, policy.OnFailureAction,
		false, policy.OnSuccessRecover, policy.OnSuccessEnableIfDisabled, policy.MaxRunHistory,
		policy.NextRunAt,
	)
	return scanAccountHealthPolicy(row)
}

func (r *accountHealthPolicyRepository) DeleteByGroupID(ctx context.Context, groupID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_health_policies WHERE group_id = $1`, groupID)
	return err
}

func (r *accountHealthPolicyRepository) UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_health_policies
		SET last_run_at = $2, next_run_at = $3, updated_at = NOW()
		WHERE id = $1
	`, id, lastRunAt, nextRunAt)
	return err
}

type accountHealthRunRepository struct {
	db *sql.DB
}

func NewAccountHealthRunRepository(db *sql.DB) service.AccountHealthRunRepository {
	return &accountHealthRunRepository{db: db}
}

const accountHealthRunSelectCols = `
	id, policy_id, group_id, trigger, status,
	total_count, success_count, failure_count, skipped_count, action_count,
	error_message, started_at, finished_at, created_at
`

func (r *accountHealthRunRepository) CreateRun(ctx context.Context, run *service.AccountHealthRun) (*service.AccountHealthRun, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO account_health_runs (
			policy_id, group_id, trigger, status,
			total_count, success_count, failure_count, skipped_count, action_count,
			error_message, started_at, created_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9,
			$10, $11, NOW()
		)
		RETURNING `+accountHealthRunSelectCols+`
	`,
		run.PolicyID, run.GroupID, run.Trigger, run.Status,
		run.TotalCount, run.SuccessCount, run.FailureCount, run.SkippedCount, run.ActionCount,
		run.ErrorMessage, run.StartedAt,
	)
	return scanAccountHealthRun(row)
}

func (r *accountHealthRunRepository) FinishRun(ctx context.Context, run *service.AccountHealthRun) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_health_runs
		SET status = $2,
		    total_count = $3,
		    success_count = $4,
		    failure_count = $5,
		    skipped_count = $6,
		    action_count = $7,
		    error_message = $8,
		    finished_at = $9
		WHERE id = $1
	`, run.ID, run.Status, run.TotalCount, run.SuccessCount, run.FailureCount, run.SkippedCount, run.ActionCount, run.ErrorMessage, run.FinishedAt)
	return err
}

func (r *accountHealthRunRepository) CreateItem(ctx context.Context, item *service.AccountHealthRunItem) (*service.AccountHealthRunItem, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO account_health_run_items (
			run_id, account_id, account_name, model_id, status, latency_ms,
			error_message, consecutive_failures, action_taken, response_excerpt,
			started_at, finished_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, NOW()
		)
		RETURNING id, run_id, account_id, account_name, model_id, status, latency_ms,
			error_message, consecutive_failures, action_taken, response_excerpt,
			started_at, finished_at, created_at
	`,
		item.RunID, item.AccountID, item.AccountName, item.ModelID, item.Status, item.LatencyMs,
		item.ErrorMessage, item.ConsecutiveFailures, item.ActionTaken, item.ResponseExcerpt,
		item.StartedAt, item.FinishedAt,
	)
	out := &service.AccountHealthRunItem{}
	if err := row.Scan(
		&out.ID, &out.RunID, &out.AccountID, &out.AccountName, &out.ModelID, &out.Status, &out.LatencyMs,
		&out.ErrorMessage, &out.ConsecutiveFailures, &out.ActionTaken, &out.ResponseExcerpt,
		&out.StartedAt, &out.FinishedAt, &out.CreatedAt,
	); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *accountHealthRunRepository) ListRunsByPolicyID(ctx context.Context, policyID int64, limit int) ([]*service.AccountHealthRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+accountHealthRunSelectCols+`
		FROM account_health_runs
		WHERE policy_id = $1
		ORDER BY started_at DESC
		LIMIT $2
	`, policyID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanAccountHealthRuns(rows)
}

func (r *accountHealthRunRepository) ListRunsByGroupID(ctx context.Context, groupID int64, limit int) ([]*service.AccountHealthRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+accountHealthRunSelectCols+`
		FROM account_health_runs
		WHERE group_id = $1
		ORDER BY started_at DESC
		LIMIT $2
	`, groupID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanAccountHealthRuns(rows)
}

func (r *accountHealthRunRepository) GetRun(ctx context.Context, id int64) (*service.AccountHealthRun, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+accountHealthRunSelectCols+`
		FROM account_health_runs WHERE id = $1
	`, id)
	return scanAccountHealthRun(row)
}

func (r *accountHealthRunRepository) ListItemsByRunID(ctx context.Context, runID int64) ([]*service.AccountHealthRunItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, run_id, account_id, account_name, model_id, status, latency_ms,
			error_message, consecutive_failures, action_taken, response_excerpt,
			started_at, finished_at, created_at
		FROM account_health_run_items
		WHERE run_id = $1
		ORDER BY id ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []*service.AccountHealthRunItem
	for rows.Next() {
		item := &service.AccountHealthRunItem{}
		if err := rows.Scan(
			&item.ID, &item.RunID, &item.AccountID, &item.AccountName, &item.ModelID, &item.Status, &item.LatencyMs,
			&item.ErrorMessage, &item.ConsecutiveFailures, &item.ActionTaken, &item.ResponseExcerpt,
			&item.StartedAt, &item.FinishedAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *accountHealthRunRepository) PruneOldRuns(ctx context.Context, policyID int64, keepCount int) error {
	if keepCount <= 0 {
		keepCount = 50
	}
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM account_health_runs
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY policy_id ORDER BY started_at DESC) AS rn
				FROM account_health_runs
				WHERE policy_id = $1
			) ranked
			WHERE rn > $2
		)
	`, policyID, keepCount)
	return err
}

type accountHealthScannable interface {
	Scan(dest ...any) error
}

func scanAccountHealthPolicy(row accountHealthScannable) (*service.AccountHealthPolicy, error) {
	p := &service.AccountHealthPolicy{}
	var preferredRaw []byte
	if err := row.Scan(
		&p.ID, &p.GroupID, &p.Enabled, &p.CronExpression, &p.ModelID, &preferredRaw,
		&p.Concurrency, &p.TimeoutSeconds, &p.ConsecutiveFailureThreshold, &p.OnFailureAction,
		&p.AllowDelete, &p.OnSuccessRecover, &p.OnSuccessEnableIfDisabled, &p.MaxRunHistory,
		&p.LastRunAt, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(preferredRaw) > 0 {
		_ = json.Unmarshal(preferredRaw, &p.PreferredModels)
	}
	if p.PreferredModels == nil {
		p.PreferredModels = []string{}
	}
	return p, nil
}

func scanAccountHealthPolicies(rows *sql.Rows) ([]*service.AccountHealthPolicy, error) {
	var out []*service.AccountHealthPolicy
	for rows.Next() {
		p, err := scanAccountHealthPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanAccountHealthRun(row accountHealthScannable) (*service.AccountHealthRun, error) {
	run := &service.AccountHealthRun{}
	if err := row.Scan(
		&run.ID, &run.PolicyID, &run.GroupID, &run.Trigger, &run.Status,
		&run.TotalCount, &run.SuccessCount, &run.FailureCount, &run.SkippedCount, &run.ActionCount,
		&run.ErrorMessage, &run.StartedAt, &run.FinishedAt, &run.CreatedAt,
	); err != nil {
		return nil, err
	}
	return run, nil
}

func scanAccountHealthRuns(rows *sql.Rows) ([]*service.AccountHealthRun, error) {
	var out []*service.AccountHealthRun
	for rows.Next() {
		run, err := scanAccountHealthRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
