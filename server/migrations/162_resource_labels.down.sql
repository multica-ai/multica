DROP TABLE IF EXISTS skill_to_label;
DROP TABLE IF EXISTS agent_to_label;

DROP INDEX IF EXISTS issue_label_workspace_type_idx;
DROP INDEX IF EXISTS issue_label_workspace_type_name_lower_idx;

-- The pre-162 schema can only represent issue labels. Resource-scoped rows
-- must be removed before restoring its workspace-wide unique name index.
DELETE FROM issue_label WHERE resource_type <> 'issue';

ALTER TABLE issue_label
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS resource_type;

CREATE UNIQUE INDEX issue_label_workspace_name_lower_idx
    ON issue_label (workspace_id, LOWER(name));
