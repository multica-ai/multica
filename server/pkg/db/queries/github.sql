-- =====================
-- GitHub Installation
-- =====================

-- name: ListGitHubInstallationsByWorkspace :many
SELECT * FROM github_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListGitHubInstallationsByInstallationID :many
-- One installation_id can be bound to several workspaces; webhook routing lists
-- every binding and fans the event out to each bound workspace. Ordered oldest
-- first so processing is deterministic and replay-stable.
SELECT * FROM github_installation
WHERE installation_id = $1
ORDER BY created_at ASC, id ASC;

-- name: GetGitHubInstallationByID :one
SELECT * FROM github_installation
WHERE id = $1;

-- name: CreateGitHubInstallation :one
INSERT INTO github_installation (
    workspace_id, installation_id, account_login, account_type, account_avatar_url, connected_by_id
) VALUES (
    $1, $2, $3, $4, sqlc.narg('account_avatar_url'), sqlc.narg('connected_by_id')
)
ON CONFLICT (workspace_id, installation_id) DO UPDATE SET
    account_login = EXCLUDED.account_login,
    account_type = EXCLUDED.account_type,
    account_avatar_url = EXCLUDED.account_avatar_url,
    connected_by_id = EXCLUDED.connected_by_id,
    updated_at = now()
RETURNING *;

-- name: DeleteGitHubInstallation :exec
DELETE FROM github_installation WHERE id = $1 AND workspace_id = $2;

-- name: DeleteGitHubInstallationByInstallationID :many
-- GitHub-side uninstall/suspend removes trust in the installation entirely, so
-- drop every workspace binding. Returns one row per deleted binding so the
-- handler can broadcast to each affected workspace.
DELETE FROM github_installation WHERE installation_id = $1
RETURNING id, workspace_id;

-- name: UpdateGitHubInstallationAccountByInstallationID :many
-- Refresh the GitHub account display metadata across every workspace binding of
-- an installation (fired by installation.created/new_permissions_accepted/
-- unsuspend). Leaves workspace_id and connected_by_id untouched.
UPDATE github_installation
SET account_login = $2,
    account_type = $3,
    account_avatar_url = sqlc.narg('account_avatar_url'),
    updated_at = now()
WHERE installation_id = $1
RETURNING *;

-- name: UpsertPendingGitHubInstallation :one
INSERT INTO github_pending_installation (
    installation_id, account_login, account_type, account_avatar_url
) VALUES (
    $1, $2, $3, sqlc.narg('account_avatar_url')
)
ON CONFLICT (installation_id) DO UPDATE SET
    account_login = EXCLUDED.account_login,
    account_type = EXCLUDED.account_type,
    account_avatar_url = EXCLUDED.account_avatar_url,
    updated_at = now()
RETURNING *;

-- name: DeletePendingGitHubInstallation :exec
DELETE FROM github_pending_installation WHERE installation_id = $1;

-- name: GetPendingGitHubInstallation :one
SELECT * FROM github_pending_installation WHERE installation_id = $1
;

-- =====================
-- GitHub Pull Request
-- =====================

-- name: UpsertGitHubPullRequest :one
-- mergeable_state has three-state semantics on UPDATE:
--   1. clear_mergeable_state=true → write NULL (state-changing actions like
--      opened/synchronize/reopened/edited(base) invalidate the prior verdict).
--   2. clear_mergeable_state=false, mergeable_state non-null → write the value.
--   3. clear_mergeable_state=false, mergeable_state null → preserve existing
--      column. Metadata events (labeled/assigned/etc.) ship payloads without
--      mergeability, and silently clobbering a known clean/dirty would lose
--      information that GitHub only re-computes lazily.
-- INSERT path always writes the incoming value (NULL acceptable for a new row).
--
-- GitHub may deliver events out of order. An event older than the stored row
-- (pr_updated_at) updates nothing and returns no row, so a late "opened" can't
-- roll a merged PR back to open — the same guard UpsertVCSPullRequest has.
INSERT INTO github_pull_request (
    workspace_id, installation_id, repo_owner, repo_name, pr_number,
    title, state, html_url, branch, author_login, author_avatar_url,
    merged_at, closed_at, pr_created_at, pr_updated_at,
    head_sha, mergeable_state,
    additions, deletions, changed_files
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, sqlc.narg('branch'), sqlc.narg('author_login'), sqlc.narg('author_avatar_url'),
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $9, $10,
    $11, sqlc.narg('mergeable_state'),
    $12, $13, $14
)
ON CONFLICT (workspace_id, repo_owner, repo_name, pr_number) DO UPDATE SET
    installation_id = EXCLUDED.installation_id,
    title = EXCLUDED.title,
    state = EXCLUDED.state,
    html_url = EXCLUDED.html_url,
    branch = EXCLUDED.branch,
    author_login = EXCLUDED.author_login,
    author_avatar_url = EXCLUDED.author_avatar_url,
    merged_at = EXCLUDED.merged_at,
    closed_at = EXCLUDED.closed_at,
    pr_updated_at = EXCLUDED.pr_updated_at,
    head_sha = EXCLUDED.head_sha,
    mergeable_state = CASE
        WHEN COALESCE(sqlc.narg('clear_mergeable_state')::boolean, FALSE) THEN NULL
        WHEN EXCLUDED.mergeable_state IS NOT NULL THEN EXCLUDED.mergeable_state
        ELSE github_pull_request.mergeable_state
    END,
    additions     = EXCLUDED.additions,
    deletions     = EXCLUDED.deletions,
    changed_files = EXCLUDED.changed_files,
    updated_at = now()
WHERE EXCLUDED.pr_updated_at >= github_pull_request.pr_updated_at
RETURNING *;

-- name: GetGitHubPullRequest :one
SELECT * FROM github_pull_request
WHERE workspace_id = $1 AND repo_owner = $2 AND repo_name = $3 AND pr_number = $4;

-- name: ListPullRequestsByIssue :many
-- Returns the issue's linked PRs with the GitHub API snapshot (MUL-5265): the
-- mergeability verdict, the CI rollup, and per-check counts for the PR's
-- CURRENT snapshot head SHA. Checks are aggregated from
-- github_pull_request_check_run — the run-level snapshot written by the API
-- refresh pipeline — NOT the legacy suite-level webhook aggregation, which is
-- removed. The `issue_prs` CTE narrows to this issue's PR ids first so the
-- aggregation only touches check rows for those PRs. Rows for an OLD head are
-- excluded by the snapshot_head_sha filter. Every link row is a delivery PR:
-- the webhook links an identifier it read from the PR title or branch name, and
-- a member can link one by hand (linked_by_type = 'member'). MUL-7429.
WITH issue_prs AS (
    SELECT pr.id, pr.snapshot_head_sha
    FROM github_pull_request pr
    JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
    WHERE ipr.issue_id = sqlc.arg('issue_id')
),
checks AS (
    SELECT
        cr.pr_id,
        COUNT(*)::bigint AS total,
        SUM(CASE WHEN cr.status = 'completed' AND cr.conclusion IN
                ('failure','cancelled','timed_out','action_required','startup_failure','stale','error')
            THEN 1 ELSE 0 END)::bigint AS failed,
        SUM(CASE WHEN cr.status = 'completed' AND cr.conclusion IN
                ('success','neutral','skipped')
            THEN 1 ELSE 0 END)::bigint AS passed,
        SUM(CASE WHEN cr.status <> 'completed' OR cr.conclusion IS NULL
            THEN 1 ELSE 0 END)::bigint AS running,
        COALESCE(
            array_agg(cr.name) FILTER (WHERE cr.status = 'completed' AND cr.conclusion IN
                ('failure','cancelled','timed_out','action_required','startup_failure','stale','error')),
            '{}'
        )::text[] AS failed_names
    FROM github_pull_request_check_run cr
    JOIN issue_prs ip ON ip.id = cr.pr_id
    WHERE cr.head_sha = ip.snapshot_head_sha AND ip.snapshot_head_sha <> ''
    GROUP BY cr.pr_id
)
SELECT
    pr.id, pr.workspace_id, pr.installation_id, pr.repo_owner, pr.repo_name,
    pr.pr_number, pr.title, pr.state, pr.html_url, pr.branch, pr.author_login,
    pr.author_avatar_url, pr.merged_at, pr.closed_at, pr.pr_created_at,
    pr.pr_updated_at, pr.head_sha, pr.mergeable_state,
    pr.additions, pr.deletions, pr.changed_files,
    pr.api_mergeable, pr.api_merge_state_status, pr.checks_rollup_state,
    pr.snapshot_head_sha, pr.snapshot_fetched_at,
    pr.created_at, pr.updated_at,
    COALESCE(ipr.linked_by_type, 'system')::text AS linked_by_type,
    COALESCE(c.total, 0)::bigint   AS checks_total,
    COALESCE(c.passed, 0)::bigint  AS checks_passed,
    COALESCE(c.failed, 0)::bigint  AS checks_failed,
    COALESCE(c.running, 0)::bigint AS checks_running,
    COALESCE(c.failed_names, '{}')::text[] AS failed_check_names
FROM github_pull_request pr
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
LEFT JOIN checks c ON c.pr_id = pr.id
WHERE ipr.issue_id = sqlc.arg('issue_id')
ORDER BY pr.pr_created_at DESC;

-- name: GetIssueReviewHeadSha :one
-- Returns the head SHA of the commit currently "under review" for an issue:
-- the most-recently-updated linked PR that still has an open/draft state and a
-- non-empty head_sha. Used by the reviewer-loop dedup (TEN-356) so a pending
-- review task pinned to an old head does not satisfy a request after HEAD
-- advanced. Prefers in-flight PRs (open/draft) over merged/closed ones so a
-- stale merged sibling can't shadow the live review target; falls back to the
-- newest linked PR with a head_sha when none are open. Returns no rows (empty
-- string) when the issue has no linked PR — callers treat that as "no SHA key"
-- and dedup on (issue_id, agent_id) alone, preserving pre-TEN-356 behavior.
--
-- Spans both GitHub and self-hosted VCS PRs: a self-hosted PR pushing a new
-- commit must move the dedup head SHA the same way a GitHub PR does, otherwise
-- a fresh review round could be merged away against a stale key.
SELECT head_sha FROM (
    SELECT pr.head_sha AS head_sha, pr.state AS state, pr.pr_updated_at AS pr_updated_at
    FROM github_pull_request pr
    JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
    WHERE ipr.issue_id = $1 AND pr.head_sha <> ''
    UNION ALL
    SELECT pr.head_sha AS head_sha, pr.state AS state, pr.pr_updated_at AS pr_updated_at
    FROM vcs_pull_request pr
    JOIN issue_vcs_pull_request ipr ON ipr.pull_request_id = pr.id
    WHERE ipr.issue_id = $1 AND pr.head_sha <> ''
) combined
ORDER BY (state IN ('open', 'draft')) DESC, pr_updated_at DESC
LIMIT 1;

-- name: ListIssueIDsForPullRequest :many
SELECT issue_id FROM issue_pull_request
WHERE pull_request_id = $1;

-- =====================
-- Issue ↔ Pull Request link
-- =====================

-- name: LinkIssueToPullRequest :execrows
-- Automatic link from a PR title, branch, or closing keyword. Returns 1 only
-- when the link is new, so the webhook evaluates auto-complete on the link
-- event and not on every redelivery. An existing link (automatic or manual) is
-- left untouched; close_intent is set by SyncPullRequestCloseIntent.
INSERT INTO issue_pull_request (
    issue_id, pull_request_id, linked_by_type, linked_by_id
) VALUES (
    $1, $2, 'system', NULL
)
ON CONFLICT (issue_id, pull_request_id) DO NOTHING;

-- name: SyncPullRequestCloseIntent :exec
-- Sets close_intent on every link of the PR, automatic or manual, to whether
-- the PR text closes that issue with a keyword ("Closes MUL-1" in its title or
-- body), so a keyword removed before the merge stops counting. The webhook
-- calls it until the PR's merge/close event, which fixes the decision.
UPDATE issue_pull_request
SET close_intent = (issue_id = ANY(sqlc.arg('closing_issue_ids')::uuid[]))
WHERE pull_request_id = sqlc.arg('pull_request_id')
  AND close_intent <> (issue_id = ANY(sqlc.arg('closing_issue_ids')::uuid[]));

-- name: LinkIssueToPullRequestManually :execrows
-- A member linked this PR by hand. Marking an existing automatic link as
-- manual keeps a later title edit from removing it. Returns 1 when the row was
-- inserted or converted.
INSERT INTO issue_pull_request (
    issue_id, pull_request_id, linked_by_type, linked_by_id
) VALUES (
    $1, $2, 'member', sqlc.narg('linked_by_id')
)
ON CONFLICT (issue_id, pull_request_id) DO UPDATE SET
    linked_by_type = 'member',
    linked_by_id = EXCLUDED.linked_by_id
WHERE issue_pull_request.linked_by_type IS DISTINCT FROM 'member';

-- name: ListAutoLinkedIssueIDsForPullRequest :many
-- Issues this PR is linked to automatically. Manual links are not listed: the
-- webhook reconciles only what it created itself.
SELECT issue_id FROM issue_pull_request
WHERE pull_request_id = $1
  AND COALESCE(linked_by_type, 'system') <> 'member';

-- name: UnlinkIssueFromPullRequest :execrows
DELETE FROM issue_pull_request
WHERE issue_id = $1 AND pull_request_id = $2;

-- name: GetGitHubPullRequestInWorkspace :one
SELECT * FROM github_pull_request
WHERE id = $1 AND workspace_id = $2;

-- name: FindGitHubPullRequestByURL :one
-- Resolves a pasted PR URL to a mirrored PR. The caller normalizes the URL to
-- scheme://host/owner/repo/pull/N; html_url is stored in that shape.
SELECT * FROM github_pull_request
WHERE workspace_id = $1
  AND lower(rtrim(html_url, '/')) = lower(sqlc.arg('html_url')::text)
ORDER BY pr_updated_at DESC
LIMIT 1;
