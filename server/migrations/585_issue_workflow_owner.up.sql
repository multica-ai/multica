-- Workflow owner (MUL-8120). Any member may create a workflow; its owner and
-- workspace owners/admins may edit or delete it. Nullable: the Default
-- workflow belongs to the workspace and has no owner, and code that predates
-- this column keeps inserting rows without one.
ALTER TABLE issue_workflow ADD COLUMN IF NOT EXISTS owner_id UUID;

-- Workflows created before owners were recorded go to the workspace's
-- earliest owner. Workflows are a few rows per workspace, so one statement
-- is enough.
UPDATE issue_workflow wf
SET owner_id = (
    SELECT m.user_id FROM member m
    WHERE m.workspace_id = wf.workspace_id AND m.role = 'owner'
    ORDER BY m.created_at, m.id
    LIMIT 1
)
WHERE wf.owner_id IS NULL
  AND NOT wf.is_default;
