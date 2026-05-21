-- =====================
-- GitHub Installation
-- =====================

-- name: ListGitHubInstallationsByWorkspace :many
SELECT * FROM github_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetGitHubInstallationByInstallationID :one
SELECT * FROM github_installation
WHERE installation_id = $1;

-- name: GetGitHubInstallationByID :one
SELECT * FROM github_installation
WHERE id = $1;

-- name: CreateGitHubInstallation :one
INSERT INTO github_installation (
    workspace_id, installation_id, account_login, account_type, account_avatar_url, connected_by_id
) VALUES (
    $1, $2, $3, $4, sqlc.narg('account_avatar_url'), sqlc.narg('connected_by_id')
)
ON CONFLICT (installation_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    account_login = EXCLUDED.account_login,
    account_type = EXCLUDED.account_type,
    account_avatar_url = EXCLUDED.account_avatar_url,
    connected_by_id = EXCLUDED.connected_by_id,
    updated_at = now()
RETURNING *;

-- name: DeleteGitHubInstallation :exec
DELETE FROM github_installation WHERE id = $1 AND workspace_id = $2;

-- name: DeleteGitHubInstallationByInstallationID :one
DELETE FROM github_installation WHERE installation_id = $1
RETURNING id, workspace_id;

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
RETURNING *;

-- name: GetGitHubPullRequest :one
SELECT * FROM github_pull_request
WHERE workspace_id = $1 AND repo_owner = $2 AND repo_name = $3 AND pr_number = $4;

-- name: ListPullRequestsByIssue :many
-- Returns the issue's linked PRs with the aggregated check-suite counts for
-- the PR's CURRENT head SHA. The `issue_prs` CTE narrows to this issue's PR
-- ids first so the per-app aggregation only touches suite rows for those
-- PRs — without that scoping the planner has to scan/aggregate every PR's
-- suites in the workspace before joining on issue. Per-app latest suite is
-- selected so a single app firing multiple suites on the same head doesn't
-- get counted N times. Late-arriving suites for an OLD head are stored but
-- excluded by the head_sha filter, so they can't override the new head's
-- pending view.
WITH issue_prs AS (
    SELECT pr.id, pr.head_sha
    FROM github_pull_request pr
    JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
    WHERE ipr.issue_id = sqlc.arg('issue_id')
),
per_app_latest AS (
    SELECT DISTINCT ON (cs.pr_id, cs.app_id)
        cs.pr_id, cs.app_id, cs.conclusion, cs.status
    FROM github_pull_request_check_suite cs
    JOIN issue_prs ip ON ip.id = cs.pr_id
    WHERE cs.head_sha = ip.head_sha AND ip.head_sha <> ''
    ORDER BY cs.pr_id, cs.app_id, cs.updated_at DESC
),
checks AS (
    SELECT
        pr_id,
        COUNT(*)::bigint AS total,
        SUM(CASE WHEN status = 'completed' AND conclusion IN
                ('failure','cancelled','timed_out','action_required','startup_failure','stale')
            THEN 1 ELSE 0 END)::bigint AS failed,
        SUM(CASE WHEN status = 'completed' AND conclusion IN
                ('success','neutral','skipped')
            THEN 1 ELSE 0 END)::bigint AS passed,
        SUM(CASE WHEN status <> 'completed' OR conclusion IS NULL
            THEN 1 ELSE 0 END)::bigint AS pending
    FROM per_app_latest
    GROUP BY pr_id
)
SELECT
    pr.id, pr.workspace_id, pr.installation_id, pr.repo_owner, pr.repo_name,
    pr.pr_number, pr.title, pr.state, pr.html_url, pr.branch, pr.author_login,
    pr.author_avatar_url, pr.merged_at, pr.closed_at, pr.pr_created_at,
    pr.pr_updated_at, pr.head_sha, pr.mergeable_state,
    pr.additions, pr.deletions, pr.changed_files,
    pr.created_at, pr.updated_at,
    COALESCE(c.total, 0)::bigint   AS checks_total,
    COALESCE(c.passed, 0)::bigint  AS checks_passed,
    COALESCE(c.failed, 0)::bigint  AS checks_failed,
    COALESCE(c.pending, 0)::bigint AS checks_pending
FROM github_pull_request pr
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
LEFT JOIN checks c ON c.pr_id = pr.id
WHERE ipr.issue_id = sqlc.arg('issue_id')
ORDER BY pr.pr_created_at DESC;

-- name: ListIssueIDsForPullRequest :many
SELECT issue_id FROM issue_pull_request
WHERE pull_request_id = $1;

-- name: GetSiblingPullRequestStateCountsForIssue :one
-- Returns, for the PRs linked to an issue excluding one PR by id (the PR
-- currently being processed by the webhook handler), how many are still in
-- flight (open or draft) and how many have already merged. The webhook
-- handler combines these with the current event's state to decide whether
-- to auto-advance the issue: the issue moves to done only when there is no
-- in-flight sibling AND at least one linked PR (current or sibling) merged.
SELECT
    COALESCE(SUM(CASE WHEN pr.state IN ('open', 'draft') THEN 1 ELSE 0 END), 0)::bigint AS open_count,
    COALESCE(SUM(CASE WHEN pr.state = 'merged' THEN 1 ELSE 0 END), 0)::bigint AS merged_count
FROM github_pull_request pr
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1
  AND pr.id <> $2;

-- =====================
-- GitHub PR check suite
-- =====================

-- name: UpsertPullRequestCheckSuite :exec
-- Upserts a single check_suite row keyed by (pr_id, suite_id). The WHERE
-- clause on the DO UPDATE branch prevents a late-arriving older event from
-- overwriting a newer one — same-PR/same-suite ordering protection. Late
-- events targeting an old head still land here (their head_sha is stored
-- on the row); the head_sha filter in ListPullRequestsByIssue keeps them
-- out of the current aggregate.
INSERT INTO github_pull_request_check_suite (
    pr_id, suite_id, head_sha, app_id, conclusion, status, updated_at
) VALUES (
    $1, $2, $3, $4, sqlc.narg('conclusion'), $5, $6
)
ON CONFLICT (pr_id, suite_id) DO UPDATE SET
    head_sha   = EXCLUDED.head_sha,
    app_id     = EXCLUDED.app_id,
    conclusion = EXCLUDED.conclusion,
    status     = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at
WHERE EXCLUDED.updated_at >= github_pull_request_check_suite.updated_at;

-- =====================
-- Issue ↔ Pull Request link
-- =====================

-- name: LinkIssueToPullRequest :exec
INSERT INTO issue_pull_request (
    issue_id, pull_request_id, linked_by_type, linked_by_id
) VALUES (
    $1, $2, sqlc.narg('linked_by_type'), sqlc.narg('linked_by_id')
)
ON CONFLICT (issue_id, pull_request_id) DO NOTHING;

-- name: UnlinkIssueFromPullRequest :exec
DELETE FROM issue_pull_request
WHERE issue_id = $1 AND pull_request_id = $2;
