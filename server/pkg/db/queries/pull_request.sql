-- name: ListPullRequestsByWorkspace :many
-- Newest-first by GitHub's pr_updated_at so the Kanban surfaces churning
-- PRs above stale ones. state filter is optional; NULL returns every state.
SELECT * FROM pull_request
WHERE workspace_id = $1
  AND (sqlc.narg('state')::pull_request_state IS NULL OR state = sqlc.narg('state'))
ORDER BY pr_updated_at DESC;

-- name: ListPullRequestsByProject :many
SELECT * FROM pull_request
WHERE project_id = $1
  AND (sqlc.narg('state')::pull_request_state IS NULL OR state = sqlc.narg('state'))
ORDER BY pr_updated_at DESC;

-- name: GetPullRequestByNumber :one
SELECT * FROM pull_request
WHERE workspace_id = $1 AND repo_url = $2 AND pr_number = $3;

-- name: UpsertPullRequest :one
-- Sync path: insert a freshly-fetched PR or update the existing row's mutable
-- fields. The unique key (workspace_id, repo_url, pr_number) keeps the upsert
-- idempotent: re-running SyncProject is safe and does not produce duplicates.
INSERT INTO pull_request (
    workspace_id, project_id, repo_url, pr_number, title, state, is_draft,
    author_login, author_avatar_url, base_ref, head_ref, head_sha, html_url,
    body, ci_status, review_decision, mergeable, additions, deletions,
    changed_files, labels, pr_created_at, pr_updated_at, pr_merged_at,
    pr_closed_at, fetched_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
    $17, $18, $19, $20, $21, $22, $23, $24, $25, now()
)
ON CONFLICT (workspace_id, repo_url, pr_number) DO UPDATE SET
    project_id        = EXCLUDED.project_id,
    title             = EXCLUDED.title,
    state             = EXCLUDED.state,
    is_draft          = EXCLUDED.is_draft,
    author_login      = EXCLUDED.author_login,
    author_avatar_url = EXCLUDED.author_avatar_url,
    base_ref          = EXCLUDED.base_ref,
    head_ref          = EXCLUDED.head_ref,
    head_sha          = EXCLUDED.head_sha,
    html_url          = EXCLUDED.html_url,
    body              = EXCLUDED.body,
    ci_status         = EXCLUDED.ci_status,
    review_decision   = EXCLUDED.review_decision,
    mergeable         = EXCLUDED.mergeable,
    additions         = EXCLUDED.additions,
    deletions         = EXCLUDED.deletions,
    changed_files     = EXCLUDED.changed_files,
    labels            = EXCLUDED.labels,
    pr_created_at     = EXCLUDED.pr_created_at,
    pr_updated_at     = EXCLUDED.pr_updated_at,
    pr_merged_at      = EXCLUDED.pr_merged_at,
    pr_closed_at      = EXCLUDED.pr_closed_at,
    fetched_at        = now()
RETURNING *;

-- name: CountOpenPullRequestsByProject :one
SELECT count(*)::bigint AS open_count FROM pull_request
WHERE project_id = $1 AND state = 'open';

-- name: CountOpenPullRequestsForProjects :many
-- Batched companion for the Ship Hub project list — one row per project so
-- the handler can populate badges without N+1 queries.
SELECT project_id, count(*)::bigint AS open_count
FROM pull_request
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[]) AND state = 'open'
GROUP BY project_id;
