-- Revert the 'group' kind. Any existing group rows are folded back to 'channel'
-- so the tightened constraint can re-apply without violating existing data
-- (a group is the closest multi-party kind to a channel).
UPDATE issue SET kind = 'channel' WHERE kind = 'group';

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_kind_check;

ALTER TABLE issue
    ADD CONSTRAINT issue_kind_check
    CHECK (kind IN ('issue', 'channel', 'dm'));
