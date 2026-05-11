WITH ranked_projects AS (
    SELECT
        id,
        row_number() OVER (
            PARTITION BY workspace_id, lower(title)
            ORDER BY created_at ASC, id ASC
        ) AS duplicate_rank
    FROM project
)
UPDATE project p
SET title = p.title || ' (' || ranked_projects.duplicate_rank || ')'
FROM ranked_projects
WHERE p.id = ranked_projects.id
  AND ranked_projects.duplicate_rank > 1;

CREATE UNIQUE INDEX idx_project_workspace_title_unique ON project(workspace_id, lower(title));

CREATE INDEX idx_project_resource_crm_account ON project_resource(workspace_id, resource_type, (resource_ref->>'account_id'))
WHERE resource_type = 'crm_account';
