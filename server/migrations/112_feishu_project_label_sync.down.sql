DROP TABLE IF EXISTS feishu_project_label_sync_binding;

ALTER TABLE feishu_project_integration
    DROP COLUMN IF EXISTS label_sync_rules;
