-- Drop the tombstone first: rows that were opted out become plain absences
-- again, which is what the pre-235 code means by "not subscribed".
DELETE FROM issue_subscriber WHERE unsubscribed_at IS NOT NULL;
ALTER TABLE issue_subscriber DROP COLUMN unsubscribed_at;

-- 'delegated' has no pre-235 equivalent. 'manual' is the closest surviving
-- label (a subscription that is neither creator/assignee nor derived from a
-- comment or mention) and matches how 120_autopilot_subscriber.down.sql
-- retires its own added reason.
UPDATE issue_subscriber SET reason = 'manual' WHERE reason = 'delegated';

-- Same two-step as the up migration: the UPDATE above already retired every
-- 'delegated' row, so the narrowed CHECK holds for all existing data. A
-- rollback runs under production load by definition, which is exactly when a
-- full-table ACCESS EXCLUSIVE scan is least affordable.
ALTER TABLE issue_subscriber DROP CONSTRAINT IF EXISTS issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual', 'autopilot')) NOT VALID;
ALTER TABLE issue_subscriber VALIDATE CONSTRAINT issue_subscriber_reason_check;
