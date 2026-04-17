-- Phase 2b.1: failure tracking on webhook events + global gitlab_issue_id
-- on the issue cache.

-- I-1: failure tracking lets the worker back off + dead-letter poison events.
ALTER TABLE gitlab_webhook_event
    ADD COLUMN failure_count INT NOT NULL DEFAULT 0,
    ADD COLUMN last_attempt_at TIMESTAMPTZ,
    ADD COLUMN last_error TEXT;

-- I-3: GitLab's Emoji Hook payload reports awardable_id as the GLOBAL issue
-- id (not the per-project IID). Cache the global id on the issue row so we
-- can resolve emoji events back to the right cached issue.
ALTER TABLE issue
    ADD COLUMN gitlab_issue_id BIGINT;

CREATE UNIQUE INDEX idx_issue_gitlab_global_id
    ON issue(workspace_id, gitlab_issue_id)
    WHERE gitlab_issue_id IS NOT NULL;
