DROP INDEX IF EXISTS idx_feishu_project_integration_workspace_unique;

ALTER TABLE feishu_project_integration
    ALTER COLUMN sync_story SET DEFAULT true;
