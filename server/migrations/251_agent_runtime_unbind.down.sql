-- Reverts 251_agent_runtime_unbind.up.sql.
--
-- Restoring NOT NULL requires that no unbound row exists. This down migration
-- deliberately does NOT delete unbound agents to make itself succeed: deleting
-- them is precisely the data loss the up migration removes, and an operator
-- rolling a schema back is not asking to destroy every agent whose machine was
-- retired.
--
-- So this fails loudly while unbound rows are present. To roll back, first
-- rebind the affected agents (PATCH /api/agents/:id with a runtime_id) and
-- re-point or accept the loss of history rows whose runtime is gone:
--
--   SELECT id, name FROM agent WHERE runtime_id IS NULL;
--   SELECT count(*) FROM agent_task_queue WHERE runtime_id IS NULL;
--
-- Note that a rolled-back application binary still contains the old
-- archive-then-hard-delete runtime teardown, so runtime deletion must not be
-- exercised during a rollback window.

ALTER TABLE autopilot
    DROP COLUMN IF EXISTS pause_reason;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT IF EXISTS agent_task_queue_active_requires_runtime;

ALTER TABLE agent_task_queue
    ALTER COLUMN runtime_id SET NOT NULL;

ALTER TABLE agent
    ALTER COLUMN runtime_id SET NOT NULL;
