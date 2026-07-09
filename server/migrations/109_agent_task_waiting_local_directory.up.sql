-- Adds the `waiting_local_directory` task status used by the daemon when a
-- task targets a project_resource of type `local_directory` whose path is
-- already held by another in-flight task. The daemon claims the row
-- (dispatched), discovers the path is busy, transitions to
-- waiting_local_directory, and only flips to running once it has the path
-- lock. This is the daemon → server protocol surface for the UI to render
-- "等待本地目录释放" instead of letting the row sit silently in dispatched.
--
-- wait_reason carries a short human-readable hint (e.g. the path being
-- waited on) that the UI can display alongside the status. It is set when
-- the row enters waiting_local_directory and cleared when it leaves.

ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'completed', 'failed', 'cancelled'));

ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS wait_reason TEXT;
