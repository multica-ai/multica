ALTER TABLE external_pull_request
    DROP COLUMN IF EXISTS sync_error,
    DROP COLUMN IF EXISTS last_sync_at,
    DROP COLUMN IF EXISTS sync_requested_at,
    DROP COLUMN IF EXISTS unresolved_comment_count,
    DROP COLUMN IF EXISTS comment_count,
    DROP COLUMN IF EXISTS ready_to_merge,
    DROP COLUMN IF EXISTS pr_updated_at,
    DROP COLUMN IF EXISTS pr_created_at,
    DROP COLUMN IF EXISTS author_login,
    DROP COLUMN IF EXISTS target_branch,
    DROP COLUMN IF EXISTS source_branch,
    DROP COLUMN IF EXISTS state;
