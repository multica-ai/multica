-- Restoring this constraint fails if the workspace already contains a
-- project-scoped issue number that collides with a workspace-scoped one, i.e.
-- any workspace that actually used project prefixes. Renumber or delete those
-- issues before rolling back.
ALTER TABLE issue ADD CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number);
