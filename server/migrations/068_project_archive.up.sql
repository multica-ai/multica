-- Project archive: soft-delete replacement for projects.
--
-- Hard delete still exists (DELETE /api/projects/:id) but cascades through
-- project_resource and breaks the link from issues that pointed at the
-- project (issue.project_id is set to NULL by the existing FK). Archive
-- gives a non-destructive alternative: the row stays, all child resources
-- and issue references stay, and the project is hidden from default
-- listings. Restoring is a single UPDATE.
--
-- archived_at IS NOT NULL means the project is archived. Mirrors migration
-- 031_agent_archive on the agent table.
ALTER TABLE project ADD COLUMN archived_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE project ADD COLUMN archived_by UUID DEFAULT NULL REFERENCES "user"(id);

-- Partial index on the active set so the most common query path
-- (ListProjects with default include_archived=false) stays fast.
CREATE INDEX idx_project_active ON project(workspace_id, created_at DESC)
WHERE archived_at IS NULL;
