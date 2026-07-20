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
-- pending view. reference_only links (a PR that merely mentions the issue
-- identifier in its body, with no closing keyword and no title/branch
-- reference) are filtered out — they are not working PRs for this issue.
WITH issue_prs AS (
    SELECT pr.id, pr.head_sha
    FROM github_pull_request pr
    JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
    WHERE ipr.issue_id = sqlc.arg('issue_id') AND NOT ipr.reference_only
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
WHERE ipr.issue_id = sqlc.arg('issue_id') AND NOT ipr.reference_only
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
SELECT pr.head_sha
FROM github_pull_request pr
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1 AND pr.head_sha <> ''
ORDER BY (pr.state IN ('open', 'draft')) DESC, pr.pr_updated_at DESC
LIMIT 1;

-- name: ListIssueIDsForPullRequest :many
SELECT issue_id FROM issue_pull_request
WHERE pull_request_id = $1;

-- name: GetIssuePullRequestCloseAggregate :one
-- Aggregates the issue's linked PRs into the two counts that gate
-- auto-advance: how many are still in flight (`open` or `draft`) and how
-- many merged PRs declared explicit closing intent on the link row. The
-- webhook auto-advances the issue when open_count = 0 AND
-- merged_with_close_intent_count > 0. Both the PR state and the link row
-- (with close_intent) are persisted before this query runs, so the result
-- is event-agnostic — a link-only sibling closing after a closing-keyword
-- PR has already merged still resolves the issue.
--
-- reference_only links (a PR that merely mentions the issue identifier in its
-- body) are excluded: they are hidden from the issue PR list, so they must not
-- silently gate auto-advance either. An open body-only mention would otherwise
-- keep open_count > 0 and block the issue from advancing while being invisible
-- in the UI. (reference_only rows never carry close_intent, so excluding them
-- does not change merged_with_close_intent_count.)
SELECT
    COALESCE(SUM(CASE WHEN pr.state IN ('open', 'draft') THEN 1 ELSE 0 END), 0)::bigint AS open_count,
    COALESCE(SUM(CASE WHEN pr.state = 'merged' AND ipr.close_intent THEN 1 ELSE 0 END), 0)::bigint AS merged_with_close_intent_count
FROM github_pull_request pr
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1 AND NOT ipr.reference_only;

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
-- GitHub pending check_suite (out-of-order arrival stash)
-- =====================

-- name: UpsertPendingCheckSuite :exec
-- Stashes a check_suite event whose PR row is not yet mirrored. Replayed
-- (and deleted) by DrainPendingCheckSuitesForPR once the matching
-- `pull_request` webhook lands. ON CONFLICT keeps the newest payload
-- for the same (workspace, repo, pr_number, suite_id) — repeated
-- deliveries while the PR is still missing are idempotent. The
-- suite_updated_at guard mirrors UpsertPullRequestCheckSuite so an older
-- event arriving after a newer one cannot overwrite the newer payload.
INSERT INTO github_pending_check_suite (
    workspace_id, installation_id, repo_owner, repo_name, pr_number,
    suite_id, head_sha, app_id, conclusion, status, suite_updated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, sqlc.narg('conclusion'), $9, $10
)
ON CONFLICT (workspace_id, repo_owner, repo_name, pr_number, suite_id) DO UPDATE SET
    installation_id  = EXCLUDED.installation_id,
    head_sha         = EXCLUDED.head_sha,
    app_id           = EXCLUDED.app_id,
    conclusion       = EXCLUDED.conclusion,
    status           = EXCLUDED.status,
    suite_updated_at = EXCLUDED.suite_updated_at,
    received_at      = now()
WHERE EXCLUDED.suite_updated_at >= github_pending_check_suite.suite_updated_at;

-- name: DrainPendingCheckSuitesForPR :many
-- Atomically reads + deletes all pending suites for the given PR address.
-- Caller replays each row through UpsertPullRequestCheckSuite. RETURNING
-- gives us the payloads we need without a separate SELECT, so two parallel
-- handlers racing on the same PR can't double-apply the same row.
DELETE FROM github_pending_check_suite
WHERE workspace_id = $1
  AND repo_owner   = $2
  AND repo_name    = $3
  AND pr_number    = $4
RETURNING suite_id, head_sha, app_id, conclusion, status, suite_updated_at;

-- =====================
-- Issue ↔ Pull Request link
-- =====================

-- name: LinkIssueToPullRequest :exec
-- close_intent reflects the PR's explicit close declaration at the moment
-- the webhook is allowed to update that intent. Open/edit/merge webhooks use
-- the current title/body parse result so authors can remove a closing keyword
-- before merge. Post-terminal edits can opt into preserving the stored value,
-- keeping the merge-time decision stable.
--
-- reference_only marks a link justified ONLY by a bare body mention (no closing
-- keyword, no title/branch reference). It follows the same preserve gate as
-- close_intent so a post-terminal edit can't retroactively hide a PR that did
-- the work. The issue's PR list filters these out (see ListPullRequestsByIssue).
INSERT INTO issue_pull_request (
    issue_id, pull_request_id, linked_by_type, linked_by_id, close_intent, reference_only
) VALUES (
    $1, $2, sqlc.narg('linked_by_type'), sqlc.narg('linked_by_id'), $3, sqlc.arg('reference_only')
)
ON CONFLICT (issue_id, pull_request_id) DO UPDATE SET
    close_intent = CASE
        WHEN sqlc.arg('preserve_close_intent') THEN issue_pull_request.close_intent
        ELSE EXCLUDED.close_intent
    END,
    reference_only = CASE
        WHEN sqlc.arg('preserve_close_intent') THEN issue_pull_request.reference_only
        ELSE EXCLUDED.reference_only
    END;

-- name: UnlinkIssueFromPullRequest :exec
DELETE FROM issue_pull_request
WHERE issue_id = $1 AND pull_request_id = $2;
