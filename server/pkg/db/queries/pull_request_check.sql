-- name: UpsertPullRequestCheck :one
-- Each (pr, head_sha, check name) folds to one row — the latest payload
-- wins. We bump updated_at so a debug query "what did GitHub say last"
-- has a sortable column.
INSERT INTO pull_request_check (
    workspace_id, pull_request_id, head_sha, name, conclusion, status,
    details_url, started_at, completed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (pull_request_id, head_sha, name) DO UPDATE SET
    conclusion   = EXCLUDED.conclusion,
    status       = EXCLUDED.status,
    details_url  = EXCLUDED.details_url,
    started_at   = EXCLUDED.started_at,
    completed_at = EXCLUDED.completed_at,
    updated_at   = now()
RETURNING *;

-- name: ListChecksForPRHead :many
-- The CI status derivation reads every check row for the PR's current
-- head sha (older sha rows are stale and will not influence the rollup).
SELECT * FROM pull_request_check
WHERE pull_request_id = $1 AND head_sha = $2
ORDER BY name ASC;

-- name: ListChecksForPullRequest :many
-- PR detail drawer surface — every check row for a PR regardless of
-- head sha, newest started_at first. Distinct from ListChecksForPRHead
-- (which only reads the active head_sha for the CI status rollup) so a
-- PR that's been force-pushed still shows historical CI runs in the
-- drawer.
SELECT * FROM pull_request_check
WHERE pull_request_id = $1
ORDER BY started_at DESC NULLS LAST, name ASC;

-- name: UpdatePullRequestCIStatus :one
-- Targeted update so the webhook-driven derivation doesn't have to
-- round-trip the entire row through UpsertPullRequest.
UPDATE pull_request SET
    ci_status  = $2,
    fetched_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdatePullRequestReviewDecision :one
UPDATE pull_request SET
    review_decision = $2,
    fetched_at      = now()
WHERE id = $1
RETURNING *;

-- name: UpdatePullRequestStateFromWebhook :one
-- Webhook-driven mutable-field update. Distinct from UpsertPullRequest
-- because the webhook payload doesn't always include the full
-- additions/deletions/changed_files diff stats — leaving those alone is
-- safer than zeroing them.
UPDATE pull_request SET
    title         = $2,
    state         = $3,
    is_draft      = $4,
    base_ref      = $5,
    head_ref      = $6,
    head_sha      = $7,
    body          = $8,
    mergeable     = $9,
    pr_updated_at = $10,
    pr_merged_at  = $11,
    pr_closed_at  = $12,
    fetched_at    = now()
WHERE id = $1
RETURNING *;
