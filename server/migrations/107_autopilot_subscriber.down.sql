-- Rows still carrying reason='autopilot' would violate the restored CHECK
-- constraint, so drop them first. Operators wanting an audit trail should
-- backfill reason='manual' before rolling this back.
DELETE FROM issue_subscriber WHERE reason = 'autopilot';

ALTER TABLE issue_subscriber DROP CONSTRAINT issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual'));

DROP INDEX IF EXISTS idx_autopilot_subscriber_user;
DROP TABLE IF EXISTS autopilot_subscriber;
