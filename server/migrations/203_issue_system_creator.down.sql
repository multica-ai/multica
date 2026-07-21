-- Drop platform-generated issues (and their comments, which have no FK)
-- before re-tightening the CHECK so the ADD CONSTRAINT does not fail on
-- existing system-created rows.
DELETE FROM comment WHERE issue_id IN (SELECT id FROM issue WHERE creator_type = 'system');
DELETE FROM issue WHERE creator_type = 'system';
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_creator_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_creator_type_check
    CHECK (creator_type IN ('member', 'agent'));
