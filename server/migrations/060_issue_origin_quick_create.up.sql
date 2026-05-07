-- Extend issue.origin_type to allow the quick-create flow to stamp issues with
-- origin_type='quick_create' + origin_id=<agent_task_queue.id>. The completion
-- handler uses this for a deterministic lookup of "the issue this quick-create
-- task produced" instead of "the agent's most recent issue", which races against
-- concurrent issue creates by the same agent (e.g. assignment task running
-- alongside quick-create when max_concurrent_tasks > 1).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create'));
