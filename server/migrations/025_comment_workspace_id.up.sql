ALTER TABLE comment ADD COLUMN workspace_id UUID REFERENCES workspace(id) ON DELETE CASCADE;

-- Backfill from issue.workspace_id
UPDATE comment SET workspace_id = issue.workspace_id
FROM issue WHERE comment.issue_id = issue.id;

-- Make non-nullable after backfill
ALTER TABLE comment ALTER COLUMN workspace_id SET NOT NULL;
