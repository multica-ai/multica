-- Rows still carrying reason='autopilot' would violate the restored CHECK
-- constraint. Relabel them to 'manual' (a value the restored CHECK still
-- allows) instead of deleting them, so rolling back narrows the reason taxonomy
-- without silently dropping who is subscribed. The reason column is not part of
-- issue_subscriber's PK (issue_id, user_type, user_id), so this UPDATE can never
-- collide with an existing row for the same subscriber.
UPDATE issue_subscriber SET reason = 'manual' WHERE reason = 'autopilot';

ALTER TABLE issue_subscriber DROP CONSTRAINT issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual'));

DROP INDEX IF EXISTS idx_autopilot_subscriber_user;
DROP TABLE IF EXISTS autopilot_subscriber;
