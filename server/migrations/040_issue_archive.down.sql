-- Revert issue archive changes.
DROP INDEX idx_issue_archived_at;
DROP INDEX idx_issue_archived_by;
ALTER TABLE issue DROP COLUMN archived_at;
ALTER TABLE issue DROP COLUMN archived_by;
