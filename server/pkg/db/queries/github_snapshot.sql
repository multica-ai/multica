-- =====================
-- GitHub API snapshot (MUL-5265, Plan C)
--
-- These queries back the API-snapshot refresh pipeline. The GitHub GraphQL
-- response is the single source of truth; each successful fetch is written as
-- one atomic batch replace (guarded update of the PR row + full replace of the
-- per-check rows) inside a single transaction.
-- =====================

-- name: ListGitHubPRRowsByAddress :many
-- One (installation, owner, repo, number) address can map to several
-- github_pull_request rows — the same installation can be bound to multiple
-- workspaces (#4823/#4855), each mirroring its own row. A single API fetch is
-- applied to every matching row (each guarded by its own head_sha).
SELECT id, workspace_id, head_sha, state
FROM github_pull_request
WHERE installation_id = $1 AND repo_owner = $2 AND repo_name = $3 AND pr_number = $4;

-- name: UpdateGitHubPRSnapshot :execrows
-- Head-SHA anti-stale write (acceptance criterion 1): the snapshot is written
-- only when the row's current head_sha still equals the head the snapshot was
-- fetched for. If the head advanced (a newer push landed while this request was
-- in flight, mirrored by the pull_request webhook), 0 rows are updated and the
-- caller discards the whole response — the per-check replace is skipped too.
UPDATE github_pull_request
SET api_mergeable          = sqlc.narg('api_mergeable'),
    api_merge_state_status = sqlc.narg('api_merge_state_status'),
    checks_rollup_state    = sqlc.narg('checks_rollup_state'),
    snapshot_head_sha      = sqlc.arg('head_sha'),
    snapshot_fetched_at    = sqlc.arg('fetched_at'),
    updated_at             = now()
WHERE id = sqlc.arg('pr_id') AND head_sha = sqlc.arg('head_sha');

-- name: DeleteGitHubPRCheckRuns :exec
-- First half of the atomic per-check replace. Runs inside the same transaction
-- as UpdateGitHubPRSnapshot and the inserts below.
DELETE FROM github_pull_request_check_run WHERE pr_id = $1;

-- name: InsertGitHubPRCheckRun :exec
INSERT INTO github_pull_request_check_run (
    pr_id, head_sha, ordinal, name, status, conclusion, details_url, is_status_context
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('conclusion'), sqlc.narg('details_url'), $6
);

-- name: ListStaleOpenGitHubPRs :many
-- TTL / safety-net sweep source. Returns distinct addresses of open/draft PRs
-- whose snapshot is missing or older than the TTL cutoff. Bounded by LIMIT so
-- one sweep can never fan out unbounded. Open PRs whose base branch advanced
-- (a conflict-producing event that emits NO pull_request webhook on this PR)
-- are recovered here without needing Contents:read. Merged/closed PRs are
-- excluded — a settled PR leaves the refresh set.
SELECT DISTINCT installation_id, repo_owner, repo_name, pr_number
FROM github_pull_request
WHERE state IN ('open', 'draft')
  AND (snapshot_fetched_at IS NULL OR snapshot_fetched_at < sqlc.arg('older_than'))
ORDER BY installation_id, repo_owner, repo_name, pr_number
LIMIT sqlc.arg('max_rows');

-- name: ListGitHubPRNumbersByHeadSHA :many
-- Resolves a commit SHA to the PR numbers whose head it is. `status` webhook
-- events (legacy commit statuses) carry a SHA + repo but no PR number, so we
-- map back through the mirrored head_sha to find which PR(s) to refresh.
SELECT DISTINCT pr_number
FROM github_pull_request
WHERE installation_id = $1 AND repo_owner = $2 AND repo_name = $3 AND head_sha = $4;

-- name: GetGitHubPullRequestByID :one
SELECT * FROM github_pull_request WHERE id = $1;
