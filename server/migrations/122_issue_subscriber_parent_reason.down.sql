-- Revert the 'parent' reason extension. Any rows with reason='parent'
-- (inherited parent subscribers) must be deleted first, since they would
-- violate the restored narrower whitelist.
DELETE FROM issue_subscriber WHERE reason = 'parent';

ALTER TABLE issue_subscriber
    DROP CONSTRAINT IF EXISTS issue_subscriber_reason_check,
    ADD CONSTRAINT issue_subscriber_reason_check
        CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual'));
