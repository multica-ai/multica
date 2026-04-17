DROP INDEX IF EXISTS idx_issue_gitlab_global_id;
ALTER TABLE issue DROP COLUMN IF EXISTS gitlab_issue_id;

ALTER TABLE gitlab_webhook_event
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS last_attempt_at,
    DROP COLUMN IF EXISTS failure_count;
