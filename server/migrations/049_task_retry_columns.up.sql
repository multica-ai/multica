-- Reconcile schema drift: production already has these columns on
-- agent_task_queue (added outside the migration system at some point), but
-- they were never committed back as a migration. This brings the repo
-- schema in line with prod and unblocks the retry-logic feature that needs
-- attempt/max_attempts/parent_task_id/failure_reason.
--
-- All ADDs use IF NOT EXISTS so this is a no-op on environments that
-- already have the columns (e.g. prod) and a true add on environments
-- that don't (e.g. local dev, CI).

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS attempt           INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS max_attempts      INTEGER NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS parent_task_id    UUID
        REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS failure_reason    TEXT,
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_task_queue_parent
    ON agent_task_queue(parent_task_id);
