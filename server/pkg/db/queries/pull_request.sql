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

-- name: UpdatePullRequestLinkage :one
-- Phase 4: persist the linkage classifier columns. nargs leave the column
-- unchanged when absent so a partial PATCH from the manual-override
-- endpoint doesn't clobber auto-detected fields.
UPDATE pull_request SET
    originating_issue_id      = sqlc.narg('originating_issue_id'),
    originating_agent_task_id = sqlc.narg('originating_agent_task_id'),
    auto_close_issue_on_merge = COALESCE(sqlc.narg('auto_close_issue_on_merge')::bool, auto_close_issue_on_merge),
    source                    = COALESCE(sqlc.narg('source')::text, source),
    fetched_at                = now()
WHERE id = $1
RETURNING *;

-- name: UpdatePullRequestConversationChannel :exec
UPDATE pull_request SET conversation_channel_id = $2, fetched_at = now() WHERE id = $1;

-- name: UpdatePullRequestStackParent :exec
UPDATE pull_request SET stack_parent_pr_id = sqlc.narg('stack_parent_pr_id'), fetched_at = now() WHERE id = $1;

-- name: ListPullRequestsByOriginatingIssue :many
-- Drives the "Linked PRs" panel on the issue detail page. Newest-first
-- so a stack of related PRs surfaces the latest activity at the top.
SELECT * FROM pull_request
WHERE workspace_id = $1 AND originating_issue_id = $2
ORDER BY pr_updated_at DESC;

-- name: ListPullRequestStackChildren :many
-- PR detail drawer — every PR rebased onto the given parent. Ordered by
-- pr_number ASC so the drawer renders children in the order the user
-- opened them. Workspace-scoped so a stale FK from a deleted workspace
-- can't surface a row that doesn't belong to the caller.
SELECT * FROM pull_request
WHERE workspace_id = $1 AND stack_parent_pr_id = $2
ORDER BY pr_number ASC;

-- name: ListOpenPullRequestsByProjectForStack :many
-- Stack detection scans every open PR in a project so the in-memory
-- joiner can match each PR's base_ref against another PR's head_ref.
-- Open-only because closed/merged PRs aren't part of the active stack.
SELECT id, project_id, head_ref, base_ref, pr_number, title
FROM pull_request
WHERE project_id = $1 AND state = 'open'
ORDER BY pr_created_at ASC;

-- name: GetPullRequestByConversationChannel :one
-- Reverse lookup: given a channel id, what PR (if any) is it attached
-- to? Used by the channel page header to render the "this channel is
-- the conversation for PR #N" badge.
SELECT * FROM pull_request
WHERE conversation_channel_id = $1;

-- name: UpdatePullRequestRiskProfile :one
-- Phase 5 — persist the rule-based classifier verdict. risk_classified_at
-- is stamped to NOW() on every call so an "is this stale?" check is a
-- simple timestamp comparison. risk_reasons is the JSONB array from the
-- classifier; storing verbatim avoids a parse step on the read path.
UPDATE pull_request SET
    risk_level         = $2,
    risk_reasons       = $3,
    risk_classified_at = NOW()
WHERE id = $1
RETURNING *;

-- name: ListPullRequestsForRiskBackfill :many
-- The webhook path runs the classifier inline; this query backfills rows
-- the reconciler imported before risk_classified_at was set. Open PRs
-- only — closed/merged rows aren't actionable for the badge.
SELECT * FROM pull_request
WHERE workspace_id = $1
  AND state = 'open'
  AND risk_classified_at IS NULL
ORDER BY pr_updated_at DESC
LIMIT $2;

-- name: CountWorkspacePullRequestsByRisk :many
-- Powers the ambient sidebar widget's "failing" segment — high+critical
-- open PRs are the urgent ones. Returns one row per risk tier present.
SELECT risk_level, count(*)::bigint AS pr_count
FROM pull_request
WHERE workspace_id = $1 AND state = 'open'
GROUP BY risk_level;

-- name: ListPullRequestsCreatedAt :many
-- Phase 5 time-machine: reconstruct the project's PR snapshot as of
-- timestamp $2. We approximate "the row's state at that timestamp" with
-- created_at <= $2 — the row's mutable fields will reflect the latest
-- sync, but for the column-derivation surface (open vs merged etc.)
-- the timestamp filter is enough. PRs that landed AFTER $2 are
-- excluded entirely.
SELECT * FROM pull_request
WHERE project_id = $1
  AND pr_created_at <= sqlc.arg('at')::timestamptz
  AND (pr_closed_at IS NULL OR pr_closed_at > sqlc.arg('at')::timestamptz)
ORDER BY pr_updated_at DESC;
