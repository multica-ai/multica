package handler

import (
	"fmt"
	"strconv"
)

// The "awaiting merge" built-in view lists issues that still have an open
// pull request linked, grouped by what stands between those PRs and a merge.
// Links come from issue_pull_request (GitHub) and issue_vcs_pull_request
// (Forgejo / Gitea / GitLab); a link the user removed survives only as an
// issue_pull_request_exclusion row and must never count. Drafts, merged and
// closed PRs are not open.

const (
	pullRequestStateReady          = "ready"
	pullRequestStateNeedsAttention = "needs_attention"
	pullRequestStateChecking       = "checking"
	// pullRequestStateNone is an issue with no open linked PR. The awaiting
	// merge scope never yields it; another scope grouped this way can.
	pullRequestStateNone = "none"

	pullRequestStateGroupKeyPrefix = "pull_request_state:"
)

var pullRequestStates = []string{
	pullRequestStateReady,
	pullRequestStateNeedsAttention,
	pullRequestStateChecking,
	pullRequestStateNone,
}

// The schema has no foreign keys, so `pr.workspace_id = i.workspace_id` keeps a
// stale link into another workspace from deciding this issue's membership.
const openGitHubPullRequestLinks = `issue_pull_request ipr
  JOIN github_pull_request pr ON pr.id = ipr.pull_request_id
 WHERE ipr.issue_id = i.id
   AND pr.workspace_id = i.workspace_id
   AND pr.state = 'open'
   AND NOT EXISTS (
     SELECT 1 FROM issue_pull_request_exclusion x
      WHERE x.issue_id = ipr.issue_id AND x.pull_request_id = ipr.pull_request_id
   )`

const openVCSPullRequestLinks = `issue_vcs_pull_request ipr
  JOIN vcs_pull_request pr ON pr.id = ipr.pull_request_id
 WHERE ipr.issue_id = i.id
   AND pr.workspace_id = i.workspace_id
   AND pr.state = 'open'
   AND NOT EXISTS (
     SELECT 1 FROM issue_pull_request_exclusion x
      WHERE x.issue_id = ipr.issue_id AND x.pull_request_id = ipr.pull_request_id
   )`

func issueTableHasOpenPullRequestPredicate() string {
	return fmt.Sprintf("(EXISTS (SELECT 1 FROM %s) OR EXISTS (SELECT 1 FROM %s))",
		openGitHubPullRequestLinks, openVCSPullRequestLinks)
}

// githubPullRequestMergeRank ranks one open GitHub PR: 0 ready, 1 checking,
// 2 needs attention. It is derivePullRequestVerdict
// (packages/core/github/pull-request-status.ts) restated over the stored
// snapshot, so a row's group never disagrees with its pill:
//
//	failed / conflicting / behind / blocked -> 2
//	running / passed / no_checks / unknown  -> 1
//	ready (merge state clean or has_hooks)  -> 0
//
// The snapshot only counts while it describes the current head, exactly like
// currentGitHubSnapshotAvailable; without one the PR has no verdict (unknown).
// Check counts are read for the snapshot head, as ListPullRequestsByIssue does.
func githubPullRequestMergeRank(snapshotsEnabled bool) string {
	return fmt.Sprintf(`CASE
  WHEN NOT (%s AND pr.snapshot_fetched_at IS NOT NULL AND pr.snapshot_head_sha <> '' AND pr.snapshot_head_sha = pr.head_sha) THEN 1
  WHEN upper(pr.checks_rollup_state) IN ('FAILURE', 'ERROR') OR EXISTS (
    SELECT 1 FROM github_pull_request_check_run cr
     WHERE cr.pr_id = pr.id AND cr.head_sha = pr.snapshot_head_sha
       AND cr.status = 'completed'
       AND cr.conclusion IN ('failure','cancelled','timed_out','action_required','startup_failure','stale','error')
  ) THEN 2
  WHEN upper(pr.api_mergeable) = 'CONFLICTING' OR upper(pr.api_merge_state_status) = 'DIRTY' THEN 2
  WHEN upper(pr.checks_rollup_state) IN ('PENDING', 'EXPECTED') THEN 1
  WHEN COALESCE(pr.checks_rollup_state, '') <> '' AND upper(pr.checks_rollup_state) <> 'SUCCESS' AND EXISTS (
    SELECT 1 FROM github_pull_request_check_run cr
     WHERE cr.pr_id = pr.id AND cr.head_sha = pr.snapshot_head_sha
       AND (cr.status <> 'completed' OR cr.conclusion IS NULL)
  ) THEN 1
  WHEN upper(pr.api_merge_state_status) IN ('BEHIND', 'BLOCKED') THEN 2
  WHEN upper(pr.api_merge_state_status) IN ('CLEAN', 'HAS_HOOKS') THEN 0
  ELSE 1
END`, strconv.FormatBool(snapshotsEnabled))
}

// A VCS PR has no mergeability snapshot, so the pill can only ever say failed
// (a failing commit status on the current head) or something that is not
// ready yet.
const vcsPullRequestMergeRank = `CASE WHEN EXISTS (
  SELECT 1 FROM vcs_commit_status cs
   WHERE cs.connection_id = pr.connection_id AND cs.sha = pr.head_sha
     AND pr.head_sha <> '' AND cs.state = 'failed'
) THEN 2 ELSE 1 END`

// issueTablePullRequestStateExpr folds an issue's open PRs into one group: the
// worst PR wins, so any PR needing attention outranks one still checking, and
// an issue is ready only when every open PR is.
func issueTablePullRequestStateExpr(snapshotsEnabled bool) string {
	return fmt.Sprintf(`COALESCE((
  SELECT CASE MAX(prs.merge_rank) WHEN 0 THEN '%s' WHEN 1 THEN '%s' WHEN 2 THEN '%s' END
  FROM (
    SELECT %s AS merge_rank FROM %s
    UNION ALL
    SELECT %s AS merge_rank FROM %s
  ) prs
), '%s')`,
		pullRequestStateReady, pullRequestStateChecking, pullRequestStateNeedsAttention,
		githubPullRequestMergeRank(snapshotsEnabled), openGitHubPullRequestLinks,
		vcsPullRequestMergeRank, openVCSPullRequestLinks,
		pullRequestStateNone)
}

// issueTablePullRequestUpdatedExpr is the most recent update across the
// issue's open PRs; NULL when it has none.
func issueTablePullRequestUpdatedExpr() string {
	return fmt.Sprintf(`(SELECT MAX(prs.pr_updated_at) FROM (
  SELECT pr.pr_updated_at FROM %s
  UNION ALL
  SELECT pr.pr_updated_at FROM %s
) prs)`, openGitHubPullRequestLinks, openVCSPullRequestLinks)
}

func pullRequestStateOrderExpression() string {
	return fmt.Sprintf("CASE group_value WHEN '%s' THEN 0 WHEN '%s' THEN 1 WHEN '%s' THEN 2 ELSE 3 END",
		pullRequestStateReady, pullRequestStateNeedsAttention, pullRequestStateChecking)
}
