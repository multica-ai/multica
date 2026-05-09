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

-- name: GetPullRequest :one
-- Phase 3 chip handlers resolve the PR by primary key (the URL pattern is
-- /api/pull_requests/{id}/...). Workspace scope is enforced by the caller.
SELECT * FROM pull_request
WHERE id = $1;

-- name: MarkPullRequestMerged :one
-- Phase 3 optimistic update after a successful GitHub merge call. We
-- transition the local row to "merged" without waiting for the inbound
-- pull_request webhook so the chip's success state is reflected
-- immediately on the Kanban. The webhook event arrives a few seconds
-- later and lands on the same row idempotently.
UPDATE pull_request SET
    state        = 'merged',
    pr_merged_at = COALESCE(sqlc.narg('merged_at')::timestamptz, now()),
    fetched_at   = now()
WHERE id = $1
RETURNING *;

-- name: MarkPullRequestClosed :one
-- Phase 3 optimistic update after a successful GitHub close call. Same
-- rationale as MarkPullRequestMerged.
UPDATE pull_request SET
    state        = 'closed',
    pr_closed_at = COALESCE(sqlc.narg('closed_at')::timestamptz, now()),
    fetched_at   = now()
WHERE id = $1
RETURNING *;

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
