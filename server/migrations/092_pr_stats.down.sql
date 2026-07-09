ALTER TABLE github_pull_request
    DROP COLUMN IF EXISTS changed_files,
    DROP COLUMN IF EXISTS deletions,
    DROP COLUMN IF EXISTS additions;
