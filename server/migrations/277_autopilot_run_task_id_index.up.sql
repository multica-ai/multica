-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- autopilot_run.task_id is an unindexed FK with ON DELETE SET NULL, so today
-- every single agent_task_queue row deletion anywhere in the product makes
-- PostgreSQL run `UPDATE autopilot_run SET task_id = NULL WHERE task_id = $1`
-- with no index behind it. Workspace teardown detaches these references
-- explicitly per task batch (MUL-5999) and needs the index for the same reason.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_autopilot_run_task_id
    ON autopilot_run (task_id);
