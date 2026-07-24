-- 187_account_health_policy.sql
-- Group-scoped account health policies, run history, and per-account items.

CREATE TABLE IF NOT EXISTS account_health_policies (
    id                             BIGSERIAL PRIMARY KEY,
    group_id                       BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    enabled                        BOOLEAN NOT NULL DEFAULT false,
    cron_expression                VARCHAR(100) NOT NULL DEFAULT '*/30 * * * *',
    model_id                       VARCHAR(200) NOT NULL DEFAULT '',
    preferred_models               JSONB NOT NULL DEFAULT '[]'::jsonb,
    concurrency                    INT NOT NULL DEFAULT 3,
    timeout_seconds                INT NOT NULL DEFAULT 60,
    consecutive_failure_threshold  INT NOT NULL DEFAULT 2,
    on_failure_action              VARCHAR(40) NOT NULL DEFAULT 'disable_schedulable',
    allow_delete                   BOOLEAN NOT NULL DEFAULT false,
    on_success_recover             BOOLEAN NOT NULL DEFAULT true,
    on_success_enable_if_disabled  BOOLEAN NOT NULL DEFAULT true,
    max_run_history                INT NOT NULL DEFAULT 50,
    last_run_at                    TIMESTAMPTZ,
    next_run_at                    TIMESTAMPTZ,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_account_health_policies_group_id UNIQUE (group_id),
    CONSTRAINT ck_ahp_concurrency CHECK (concurrency BETWEEN 1 AND 50),
    CONSTRAINT ck_ahp_timeout CHECK (timeout_seconds BETWEEN 5 AND 600),
    CONSTRAINT ck_ahp_threshold CHECK (consecutive_failure_threshold BETWEEN 1 AND 20),
    CONSTRAINT ck_ahp_max_history CHECK (max_run_history BETWEEN 1 AND 500)
);

CREATE INDEX IF NOT EXISTS idx_ahp_enabled_next_run
    ON account_health_policies (enabled, next_run_at)
    WHERE enabled = true;

CREATE TABLE IF NOT EXISTS account_health_runs (
    id              BIGSERIAL PRIMARY KEY,
    policy_id       BIGINT NOT NULL REFERENCES account_health_policies(id) ON DELETE CASCADE,
    group_id        BIGINT NOT NULL,
    trigger         VARCHAR(20) NOT NULL DEFAULT 'cron',
    status          VARCHAR(20) NOT NULL DEFAULT 'running',
    total_count     INT NOT NULL DEFAULT 0,
    success_count   INT NOT NULL DEFAULT 0,
    failure_count   INT NOT NULL DEFAULT 0,
    skipped_count   INT NOT NULL DEFAULT 0,
    action_count    INT NOT NULL DEFAULT 0,
    error_message   TEXT NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ahr_policy_started
    ON account_health_runs (policy_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_ahr_group_started
    ON account_health_runs (group_id, started_at DESC);

CREATE TABLE IF NOT EXISTS account_health_run_items (
    id                    BIGSERIAL PRIMARY KEY,
    run_id                BIGINT NOT NULL REFERENCES account_health_runs(id) ON DELETE CASCADE,
    account_id            BIGINT NOT NULL,
    account_name          VARCHAR(255) NOT NULL DEFAULT '',
    model_id              VARCHAR(200) NOT NULL DEFAULT '',
    status                VARCHAR(20) NOT NULL DEFAULT 'failed',
    latency_ms            BIGINT NOT NULL DEFAULT 0,
    error_message         TEXT NOT NULL DEFAULT '',
    consecutive_failures  INT NOT NULL DEFAULT 0,
    action_taken          VARCHAR(40) NOT NULL DEFAULT 'none',
    response_excerpt      TEXT NOT NULL DEFAULT '',
    started_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ahri_run_id
    ON account_health_run_items (run_id, id ASC);
