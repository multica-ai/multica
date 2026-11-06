-- Revert only the new Oracle event claim/config hooks.
DROP TRIGGER IF EXISTS cancel_oracle_event_tasks_on_workspace_change ON workspace;
DROP FUNCTION IF EXISTS cancel_oracle_event_tasks_on_workspace_change();
DROP TRIGGER IF EXISTS native_oracle_issue_claim_guard ON agent_task_queue;
DROP FUNCTION IF EXISTS native_oracle_issue_claim_guard();

-- Migration 533 removed stop_issue_wakeups() entirely. Do not resurrect it.
-- Application-layer query changes roll back with the corresponding code.
