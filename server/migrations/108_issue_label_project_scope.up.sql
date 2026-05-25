-- Add project-scoped labels. Existing labels stay global (`project_id IS NULL`)
-- so old clients keep their workspace-wide behavior.

ALTER TABLE issue_label
    ADD COLUMN IF NOT EXISTS project_id UUID;

-- A composite FK guarantees a project-scoped label belongs to the same
-- workspace as its project. PostgreSQL requires a unique key on the referenced
-- column pair even though `project.id` is already globally unique.
CREATE UNIQUE INDEX IF NOT EXISTS project_workspace_id_id_uidx
    ON project (workspace_id, id);

ALTER TABLE issue_label
    DROP CONSTRAINT IF EXISTS issue_label_workspace_project_fk,
    ADD CONSTRAINT issue_label_workspace_project_fk
        FOREIGN KEY (workspace_id, project_id)
        REFERENCES project (workspace_id, id)
        ON DELETE CASCADE;

DROP INDEX IF EXISTS issue_label_workspace_name_lower_idx;

CREATE UNIQUE INDEX IF NOT EXISTS issue_label_workspace_global_name_lower_uidx
    ON issue_label (workspace_id, LOWER(name))
    WHERE project_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS issue_label_workspace_project_name_lower_uidx
    ON issue_label (workspace_id, project_id, LOWER(name))
    WHERE project_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS issue_label_workspace_name_lower_lookup_idx
    ON issue_label (workspace_id, LOWER(name));
