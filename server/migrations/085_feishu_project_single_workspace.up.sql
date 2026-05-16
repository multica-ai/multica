WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY workspace_id
               ORDER BY updated_at DESC, created_at DESC, id DESC
           ) AS rn
    FROM feishu_project_integration
)
DELETE FROM feishu_project_integration
WHERE id IN (
    SELECT id FROM ranked WHERE rn > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_feishu_project_integration_workspace_unique
    ON feishu_project_integration(workspace_id);

ALTER TABLE feishu_project_integration
    ALTER COLUMN sync_story SET DEFAULT false;

UPDATE feishu_project_integration
SET sync_story = false,
    sync_issue = true,
    mql_filter = '',
    updated_at = now();
