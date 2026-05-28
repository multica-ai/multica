ALTER TABLE feishu_project_integration
    DROP COLUMN IF EXISTS last_reconciled_at,
    DROP COLUMN IF EXISTS last_seen_updated_at_ms;
