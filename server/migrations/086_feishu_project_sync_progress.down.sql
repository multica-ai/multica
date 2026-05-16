ALTER TABLE feishu_project_sync_run
    DROP COLUMN IF EXISTS current_type,
    DROP COLUMN IF EXISTS current_page,
    DROP COLUMN IF EXISTS processed_count,
    DROP COLUMN IF EXISTS total_count;

