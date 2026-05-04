ALTER TABLE budget_change_log DROP CONSTRAINT IF EXISTS budget_change_log_scope_type_check;
ALTER TABLE budget_change_log
    ADD CONSTRAINT budget_change_log_scope_type_check
    CHECK (scope_type IN ('workspace_default','user_override','agent_override','workspace_safety'));

DROP INDEX IF EXISTS idx_agent_task_queue_triggered_by;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS triggered_by_user_id;

ALTER TABLE member DROP COLUMN IF EXISTS budget_enforcement_enabled;
ALTER TABLE member DROP COLUMN IF EXISTS scope_enforcement_enabled;
