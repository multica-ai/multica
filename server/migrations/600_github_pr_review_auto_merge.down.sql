ALTER TABLE github_pull_request
    DROP COLUMN IF EXISTS review_decision,
    DROP COLUMN IF EXISTS auto_merge_method,
    DROP COLUMN IF EXISTS auto_merge_enabled_by;
