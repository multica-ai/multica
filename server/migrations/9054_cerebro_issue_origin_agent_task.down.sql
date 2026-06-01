-- MUL-2553 (rollback): drop 'agent_task' from issue.origin_type CHECK. Any rows
-- already stamped with origin_type='agent_task' would violate the new CHECK,
-- so first clear them.
UPDATE issue SET origin_type = NULL, origin_id = NULL WHERE origin_type = 'agent_task';
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'runtime_approval'));

-- Drop 'triggered_agent' from issue_subscriber.reason CHECK. Existing rows
-- with that reason must be removed first or the new CHECK would reject them.
DELETE FROM issue_subscriber WHERE reason = 'triggered_agent';
ALTER TABLE issue_subscriber DROP CONSTRAINT IF EXISTS issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual'));
