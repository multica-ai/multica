ALTER TABLE IF EXISTS vcs_pull_request
    DROP COLUMN IF EXISTS snapshot_head_sha,
    DROP COLUMN IF EXISTS snapshot_fetched_at,
    DROP COLUMN IF EXISTS snapshot,
    DROP COLUMN IF EXISTS snapshot_error;
DROP TABLE IF EXISTS gongfeng_repository;
