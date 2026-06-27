-- =====================
-- Forgejo Connection
-- =====================

-- name: ListForgejoConnectionsByWorkspace :many
SELECT * FROM forgejo_connection
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetForgejoConnectionByID :one
SELECT * FROM forgejo_connection
WHERE id = $1;

-- name: GetForgejoConnectionForWorkspace :one
SELECT * FROM forgejo_connection
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertForgejoConnection :one
-- Reconnecting the same instance rotates the stored token/secret and identity
-- in place rather than creating a duplicate row.
INSERT INTO forgejo_connection (
    workspace_id, instance_url, account_login,
    access_token_encrypted, webhook_secret_encrypted, connected_by_id
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('connected_by_id')
)
ON CONFLICT (workspace_id, instance_url) DO UPDATE SET
    account_login            = EXCLUDED.account_login,
    access_token_encrypted   = EXCLUDED.access_token_encrypted,
    webhook_secret_encrypted = EXCLUDED.webhook_secret_encrypted,
    connected_by_id          = EXCLUDED.connected_by_id,
    updated_at               = now()
RETURNING *;

-- name: DeleteForgejoConnection :exec
DELETE FROM forgejo_connection WHERE id = $1 AND workspace_id = $2;

-- =====================
-- Forgejo Pull Request
-- =====================

-- name: UpsertForgejoPullRequest :one
INSERT INTO forgejo_pull_request (
    workspace_id, connection_id, repo_owner, repo_name, pr_number,
    title, state, html_url, branch, author_login, author_avatar_url,
    merged_at, closed_at, pr_created_at, pr_updated_at,
    additions, deletions, changed_files
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, sqlc.narg('branch'), sqlc.narg('author_login'), sqlc.narg('author_avatar_url'),
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $9, $10,
    $11, $12, $13
)
ON CONFLICT (connection_id, repo_owner, repo_name, pr_number) DO UPDATE SET
    workspace_id      = EXCLUDED.workspace_id,
    title             = EXCLUDED.title,
    state             = EXCLUDED.state,
    html_url          = EXCLUDED.html_url,
    branch            = EXCLUDED.branch,
    author_login      = EXCLUDED.author_login,
    author_avatar_url = EXCLUDED.author_avatar_url,
    merged_at         = EXCLUDED.merged_at,
    closed_at         = EXCLUDED.closed_at,
    pr_updated_at     = EXCLUDED.pr_updated_at,
    additions         = EXCLUDED.additions,
    deletions         = EXCLUDED.deletions,
    changed_files     = EXCLUDED.changed_files,
    updated_at        = now()
RETURNING *;

-- name: ListForgejoPullRequestsByIssue :many
-- Forgejo has no check-suite model yet, so check counts are reported as 0;
-- the response mapper sets ChecksConclusion to nil (frontend hides the bar).
SELECT pr.*
FROM forgejo_pull_request pr
JOIN issue_forgejo_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1
ORDER BY pr.pr_created_at DESC;

-- name: ListIssueIDsForForgejoPullRequest :many
SELECT issue_id FROM issue_forgejo_pull_request
WHERE pull_request_id = $1;

-- name: GetForgejoIssuePullRequestCloseAggregate :one
-- Mirrors GetIssuePullRequestCloseAggregate for Forgejo PRs: counts in-flight
-- (open/draft) linked PRs and merged PRs that declared close intent. The
-- webhook auto-advances the issue when open_count = 0 AND
-- merged_with_close_intent_count > 0.
SELECT
    COALESCE(SUM(CASE WHEN pr.state IN ('open', 'draft') THEN 1 ELSE 0 END), 0)::bigint AS open_count,
    COALESCE(SUM(CASE WHEN pr.state = 'merged' AND ipr.close_intent THEN 1 ELSE 0 END), 0)::bigint AS merged_with_close_intent_count
FROM forgejo_pull_request pr
JOIN issue_forgejo_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1;

-- =====================
-- Issue ↔ Forgejo PR link
-- =====================

-- name: LinkIssueToForgejoPullRequest :exec
INSERT INTO issue_forgejo_pull_request (
    issue_id, pull_request_id, linked_by_type, linked_by_id, close_intent
) VALUES (
    $1, $2, sqlc.narg('linked_by_type'), sqlc.narg('linked_by_id'), $3
)
ON CONFLICT (issue_id, pull_request_id) DO UPDATE SET
    close_intent = CASE
        WHEN sqlc.arg('preserve_close_intent') THEN issue_forgejo_pull_request.close_intent
        ELSE EXCLUDED.close_intent
    END;
