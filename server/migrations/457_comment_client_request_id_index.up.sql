-- Scoped per issue, not per workspace: retries always target the same issue,
-- and scoping the uniqueness this way keeps an agent free to reuse one key
-- scheme (e.g. "<task>:final-report") across the issues it works on.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_client_request_id
    ON comment (workspace_id, issue_id, client_request_id)
    WHERE client_request_id IS NOT NULL;
