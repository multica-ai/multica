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
    additions, deletions, changed_files, head_sha
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, sqlc.narg('branch'), sqlc.narg('author_login'), sqlc.narg('author_avatar_url'),
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $9, $10,
    $11, $12, $13, $14
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
    head_sha          = EXCLUDED.head_sha,
    updated_at        = now()
RETURNING *;

-- name: ListForgejoPullRequestsByIssue :many
-- Aggregates each PR's commit statuses for its CURRENT head sha into
-- passed/failed/pending counts. forgejo_commit_status holds one row per
-- (connection, sha, context), so a simple count by state-class is correct —
-- no per-context DISTINCT needed. Statuses for an old head sha stay stored but
-- are excluded by the head_sha join, so a stale run can't pollute the bar.
WITH checks AS (
    SELECT
        pr.id AS pr_id,
        COUNT(*)::bigint AS total,
        SUM(CASE WHEN cs.state IN ('failure','error') THEN 1 ELSE 0 END)::bigint AS failed,
        SUM(CASE WHEN cs.state IN ('success','warning') THEN 1 ELSE 0 END)::bigint AS passed,
        SUM(CASE WHEN cs.state NOT IN ('failure','error','success','warning') THEN 1 ELSE 0 END)::bigint AS pending
    FROM forgejo_pull_request pr
    JOIN issue_forgejo_pull_request ipr ON ipr.pull_request_id = pr.id
    JOIN forgejo_commit_status cs
        ON cs.connection_id = pr.connection_id
       AND cs.sha = pr.head_sha
       AND pr.head_sha <> ''
    WHERE ipr.issue_id = sqlc.arg('issue_id')
    GROUP BY pr.id
)
SELECT
    pr.*,
    COALESCE(c.total, 0)::bigint   AS checks_total,
    COALESCE(c.passed, 0)::bigint  AS checks_passed,
    COALESCE(c.failed, 0)::bigint  AS checks_failed,
    COALESCE(c.pending, 0)::bigint AS checks_pending
FROM forgejo_pull_request pr
JOIN issue_forgejo_pull_request ipr ON ipr.pull_request_id = pr.id
LEFT JOIN checks c ON c.pr_id = pr.id
WHERE ipr.issue_id = sqlc.arg('issue_id')
ORDER BY pr.pr_created_at DESC;

-- name: UpsertForgejoCommitStatus :exec
-- One row per (connection, sha, context); a redelivery or a state transition
-- overwrites in place. updated_at guards against an older event overwriting a
-- newer one for the same context.
INSERT INTO forgejo_commit_status (
    connection_id, sha, context, state, target_url, description, updated_at
) VALUES (
    $1, $2, $3, $4, sqlc.narg('target_url'), sqlc.narg('description'), $5
)
ON CONFLICT (connection_id, sha, context) DO UPDATE SET
    state       = EXCLUDED.state,
    target_url  = EXCLUDED.target_url,
    description = EXCLUDED.description,
    updated_at  = EXCLUDED.updated_at
WHERE EXCLUDED.updated_at >= forgejo_commit_status.updated_at;

-- name: ListIssueIDsForForgejoPRHead :many
-- Issues linked to any Forgejo PR whose head sha matches the given status, so
-- a commit-status event can fan out a PR-card refresh to the right issues.
SELECT DISTINCT ipr.issue_id
FROM forgejo_pull_request pr
JOIN issue_forgejo_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE pr.connection_id = $1 AND pr.head_sha = $2 AND pr.head_sha <> '';

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
