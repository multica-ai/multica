-- =====================
-- GitLab Merge Request
-- =====================

-- name: UpsertGitlabMergeRequest :one
INSERT INTO multica_gitlab_merge_request (
    workspace_id, repo_owner, repo_name, mr_number,
    mr_id, project_id, title, description, state, html_url,
    source_branch, target_branch, author_login, author_avatar_url,
    merged_at, closed_at, mr_created_at, mr_updated_at
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, sqlc.narg('description'), $8, $9,
    sqlc.narg('source_branch'), sqlc.narg('target_branch'),
    sqlc.narg('author_login'), sqlc.narg('author_avatar_url'),
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $10, $11
)
ON CONFLICT (workspace_id, repo_owner, repo_name, mr_number) DO UPDATE SET
    mr_id = EXCLUDED.mr_id,
    project_id = EXCLUDED.project_id,
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    state = EXCLUDED.state,
    html_url = EXCLUDED.html_url,
    source_branch = EXCLUDED.source_branch,
    target_branch = EXCLUDED.target_branch,
    author_login = EXCLUDED.author_login,
    author_avatar_url = EXCLUDED.author_avatar_url,
    merged_at = EXCLUDED.merged_at,
    closed_at = EXCLUDED.closed_at,
    mr_updated_at = EXCLUDED.mr_updated_at,
    updated_at = now()
RETURNING *;

-- name: GetGitlabMergeRequestByProjectAndIID :one
SELECT * FROM multica_gitlab_merge_request
WHERE workspace_id = $1 AND project_id = $2 AND mr_number = $3;

-- name: ListMergeRequestsByIssue :many
SELECT mr.*
FROM multica_gitlab_merge_request mr
JOIN multica_issue_merge_request imr ON imr.merge_request_id = mr.id
WHERE imr.issue_id = $1
ORDER BY mr.mr_created_at DESC;

-- name: ListIssueIDsForMergeRequest :many
SELECT issue_id FROM multica_issue_merge_request
WHERE merge_request_id = $1;

-- name: GetSiblingMergeRequestStateCountsForIssue :one
-- Returns, for the MRs linked to an issue excluding one MR by id (the MR
-- currently being processed by the webhook handler), how many are still
-- opened and how many have already merged. The webhook handler combines
-- these with the current event's state to decide whether to auto-advance
-- the issue: the issue moves to done only when there is no opened sibling
-- AND at least one linked MR (current or sibling) merged.
SELECT
    COALESCE(SUM(CASE WHEN mr.state = 'opened' THEN 1 ELSE 0 END), 0)::bigint AS open_count,
    COALESCE(SUM(CASE WHEN mr.state = 'merged' THEN 1 ELSE 0 END), 0)::bigint AS merged_count
FROM multica_gitlab_merge_request mr
JOIN multica_issue_merge_request imr ON imr.merge_request_id = mr.id
WHERE imr.issue_id = $1
  AND mr.id <> $2;

-- =====================
-- Issue <-> Merge Request link
-- =====================

-- name: LinkIssueToMergeRequest :exec
INSERT INTO multica_issue_merge_request (
    issue_id, merge_request_id, linked_by_type, linked_by_id
) VALUES (
    $1, $2, sqlc.narg('linked_by_type'), sqlc.narg('linked_by_id')
)
ON CONFLICT (issue_id, merge_request_id) DO NOTHING;

-- name: UnlinkIssueFromMergeRequest :exec
DELETE FROM multica_issue_merge_request
WHERE issue_id = $1 AND merge_request_id = $2;

-- =====================
-- Webhook token lookup
-- =====================

-- name: ListWorkspacesForGitlabWebhookLookup :many
SELECT id, settings FROM multica_workspace
WHERE settings IS NOT NULL AND settings::text != '{}'::text;
