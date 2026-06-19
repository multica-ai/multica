-- Schema for the DB-backed execution-record scheduler (MUL-2957). One row
-- represents a single planned execution of a registered system job for a
-- single scope at a single canonical UTC `plan_time` bucket — the row is
-- both the distributed lease and the audit log entry.
--
-- Design source: docs/db-backed-execution-scheduler-rfc.md.
--
-- Key invariants:
--
--   * The unique key (job_name, scope_kind, scope_id, plan_time) is the
--     only thing standing between two app instances and a double-run.
--     `scope_kind` / `scope_id` default to 'global' so global jobs land
--     on the same uniqueness slot without using NULL semantics.
--
--   * Status uses `TEXT + CHECK` instead of a real enum so adding a state
--     later is a CHECK swap, not an enum migration. There is no `STALE`
--     state — stale is `status='RUNNING' AND stale_after < now()`, which
--     keeps lease theft a single UPDATE.
--
--   * Every mutation that targets a live lease (heartbeat, terminal
--     write) MUST match `id = $id AND lease_token = $token AND
--     status = 'RUNNING'`. A runner that lost its lease (stolen by a
--     stale-steal, or already finalised by a sibling) sees
--     RowsAffected = 0 and stops, so it cannot rewrite a newer attempt's
--     state to SUCCESS / FAILED.
--
--   * `attempt` and `max_attempts` are bounded by CHECK so a buggy
--     handler cannot "escape" the retry envelope by writing
--     attempt > max_attempts.
--
-- This migration creates the table and the few indexes the steady-state
-- scheduler queries need (claim-by-key, find-stale-RUNNING, recent
-- failures, retention prune). It does NOT register any jobs — those are
-- declared in Go and inserted on demand by the scheduler.

CREATE TABLE sys_cron_executions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name       TEXT        NOT NULL,
    scope_kind     TEXT        NOT NULL DEFAULT 'global',
    scope_id       TEXT        NOT NULL DEFAULT 'global',
    plan_time      TIMESTAMPTZ NOT NULL,

    -- Lifecycle. status ∈ {RUNNING, SUCCESS, FAILED}; stale is computed
    -- from RUNNING + stale_after, never materialised as its own state.
    status         TEXT        NOT NULL,
    attempt        INTEGER     NOT NULL DEFAULT 1,
    max_attempts   INTEGER     NOT NULL DEFAULT 3,
    next_retry_at  TIMESTAMPTZ,

    -- Lease ownership. lease_token is rotated on every claim/steal, so a
    -- runner whose lease was stolen cannot write terminal status — its
    -- UPDATE matches zero rows.
    runner_id      TEXT,
    lease_token    UUID        NOT NULL DEFAULT gen_random_uuid(),
    heartbeat_at   TIMESTAMPTZ,
    stale_after    TIMESTAMPTZ,

    -- Audit fields. Small structured `result` only — bulky output goes to
    -- structured logs, not into this table.
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    duration_ms    INTEGER,
    rows_affected  BIGINT,
    result         JSONB       NOT NULL DEFAULT '{}'::jsonb,

    error_code     TEXT,
    error_msg      TEXT,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_sys_cron_status
        CHECK (status IN ('RUNNING', 'SUCCESS', 'FAILED')),
    CONSTRAINT chk_sys_cron_attempt
        CHECK (attempt >= 1 AND max_attempts >= attempt),
    CONSTRAINT chk_sys_cron_duration
        CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CONSTRAINT uq_sys_cron_execution
        UNIQUE (job_name, scope_kind, scope_id, plan_time)
);

-- Latest-plan lookup per (job, scope). The scheduler reads this to
-- decide whether the latest plan is already done or in flight; the
-- prune job and `latest-success` lag computation use the same key.
CREATE INDEX idx_sys_cron_exec_job_plan
    ON sys_cron_executions (job_name, scope_kind, scope_id, plan_time DESC);

-- Find leases that have gone stale without scanning the whole RUNNING
-- set. Partial because non-RUNNING rows never need to be considered.
CREATE INDEX idx_sys_cron_exec_running_stale
    ON sys_cron_executions (stale_after)
    WHERE status = 'RUNNING';

-- "Recent FAILED for this job" — used by alerting and by the retry
-- decision, which needs the most recent FAILED row to compare against
-- max_attempts and next_retry_at.
CREATE INDEX idx_sys_cron_exec_failed_recent
    ON sys_cron_executions (job_name, plan_time DESC)
    WHERE status = 'FAILED';

-- Retention prune scans by terminal `finished_at`; index is partial
-- because RUNNING rows are kept by the stale policy, not the retention
-- policy.
CREATE INDEX idx_sys_cron_exec_finished
    ON sys_cron_executions (finished_at)
    WHERE status IN ('SUCCESS', 'FAILED');
