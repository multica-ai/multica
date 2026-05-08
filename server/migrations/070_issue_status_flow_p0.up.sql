-- PUL-13 P0: status-flow rework — DDL only, no behavior changes.
-- Extends the issue.status CHECK constraint with four new lifecycle values
-- (waiting, planned, developing, deployed) and keeps in_review/done in the
-- whitelist for backward compatibility through P1 backfill + 30-day CLI alias
-- grace period. Adds deployed_at lifecycle marker. Adds issue_status_history
-- audit table with idempotency unique constraint on (source, ref_id).

BEGIN;

-- 1. Extend whitelist. Drop+Add atomically. Constraint name issue_status_check
--    is the Postgres default for an inline CHECK on column status of table
--    issue (table_column_check convention). IF EXISTS guards against env drift.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN (
        'backlog',
        'todo',
        'in_progress',
        'waiting',          -- new
        'planned',          -- new
        'developing',       -- new
        'deployed',         -- new
        'in_review',        -- legacy, removed in PR7 cleanup after backfill + grace
        'done',             -- legacy, removed in PR7 cleanup after backfill + grace
        'blocked',
        'cancelled'
    ));

-- 2. deployed_at: lifecycle marker. NULL until the first time this issue
--    reached 'deployed'. Bug-fix scenarios that move deployed → developing
--    preserve this column as evidence the issue was on prod at least once.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS deployed_at TIMESTAMPTZ NULL;

-- 3. issue_status_history: audit + idempotency. UNIQUE (source, ref_id) is the
--    deduplication contract — comment.created hook fires twice on the same
--    comment.id, Forge replays a webhook with the same deploy_id, /publish-plan
--    re-runs after a CLI failure — UNIQUE blocks the duplicate row, the second
--    write fails cleanly. from_status/to_status are TEXT (not constrained
--    CHECK) so future status-set changes don't require migrating history.
CREATE TABLE IF NOT EXISTS issue_status_history (
    id BIGSERIAL PRIMARY KEY,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    from_status TEXT,
    to_status TEXT NOT NULL,
    source VARCHAR(32) NOT NULL CHECK (source IN (
        'manual',
        'hook_comment',
        'skill_publish',
        'skill_pickup',
        'webhook_forge',
        'backfill'
    )),
    actor_id UUID,
    actor_type VARCHAR(16) CHECK (actor_type IN ('member', 'agent', 'system')),
    ref_id VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source, ref_id)
);

CREATE INDEX IF NOT EXISTS ix_issue_status_history_issue
    ON issue_status_history(issue_id, created_at DESC);

COMMIT;
