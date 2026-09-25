-- =====================
-- PR auto-complete (MUL-7429)
-- =====================

-- name: GetIssuePRAutoCompleteDisabled :one
SELECT EXISTS (
    SELECT 1 FROM issue_pr_automation
    WHERE issue_id = $1 AND auto_complete_disabled
)::boolean AS disabled;

-- name: SetIssuePRAutoCompleteDisabled :exec
INSERT INTO issue_pr_automation (
    issue_id, workspace_id, auto_complete_disabled, updated_by_type, updated_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('updated_by_type'), sqlc.narg('updated_by_id')
)
ON CONFLICT (issue_id) DO UPDATE SET
    auto_complete_disabled = EXCLUDED.auto_complete_disabled,
    updated_by_type = EXCLUDED.updated_by_type,
    updated_by_id = EXCLUDED.updated_by_id,
    updated_at = now();

-- name: IsPullRequestExcludedFromIssue :one
SELECT EXISTS (
    SELECT 1 FROM issue_pull_request_exclusion
    WHERE issue_id = $1 AND pull_request_id = $2
)::boolean AS excluded;

-- name: ExcludePullRequestFromIssue :exec
INSERT INTO issue_pull_request_exclusion (
    issue_id, pull_request_id, workspace_id, excluded_by_type, excluded_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('excluded_by_type'), sqlc.narg('excluded_by_id')
)
ON CONFLICT (issue_id, pull_request_id) DO UPDATE SET
    excluded_by_type = EXCLUDED.excluded_by_type,
    excluded_by_id = EXCLUDED.excluded_by_id,
    created_at = now();

-- name: DeletePullRequestExclusion :exec
DELETE FROM issue_pull_request_exclusion
WHERE issue_id = $1 AND pull_request_id = $2;

-- name: ListIssueLinkedPullRequestStates :many
-- Every PR linked to the issue across GitHub and self-hosted providers, for
-- the auto-complete decision. Ordered by number so reasons read stably.
SELECT pr.id, 'github'::text AS provider, pr.pr_number, pr.state, ipr.close_intent
FROM github_pull_request pr
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1
UNION ALL
SELECT pr.id, pr.provider AS provider, pr.pr_number, pr.state, ipr.close_intent
FROM vcs_pull_request pr
JOIN issue_vcs_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1
ORDER BY pr_number;

-- name: CompleteIssueFromPullRequests :one
-- Conditional status write for PR auto-complete. It lands only if the issue is
-- still in the status the decision saw (two merges racing complete it once),
-- the linked PRs are still all merged when the write runs (a PR linked between
-- the decision and this statement keeps the issue open), and one of them still
-- closes the issue with a keyword. Repositions like UpdateIssueStatus does.
UPDATE issue AS i SET
    status = 'done',
    duplicate_of_issue_id = NULL,
    position = (
        SELECT COALESCE(MIN(target.position), 0) - 1
        FROM issue AS target
        WHERE target.workspace_id = i.workspace_id
          AND target.status = 'done'
    ),
    revision = i.revision + 1,
    last_activity_at = GREATEST(COALESCE(i.last_activity_at, i.updated_at), now()),
    updated_at = now()
WHERE i.id = $1
  AND i.workspace_id = $2
  AND i.status = sqlc.arg('expected_status')::text
  AND i.status <> 'done'
  AND EXISTS (
      SELECT 1 FROM issue_pull_request ipr WHERE ipr.issue_id = i.id
      UNION ALL
      SELECT 1 FROM issue_vcs_pull_request ipr WHERE ipr.issue_id = i.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM issue_pull_request ipr
      JOIN github_pull_request pr ON pr.id = ipr.pull_request_id
      WHERE ipr.issue_id = i.id AND pr.state <> 'merged'
  )
  AND NOT EXISTS (
      SELECT 1 FROM issue_vcs_pull_request ipr
      JOIN vcs_pull_request pr ON pr.id = ipr.pull_request_id
      WHERE ipr.issue_id = i.id AND pr.state <> 'merged'
  )
  AND EXISTS (
      SELECT 1 FROM issue_pull_request ipr WHERE ipr.issue_id = i.id AND ipr.close_intent
      UNION ALL
      SELECT 1 FROM issue_vcs_pull_request ipr WHERE ipr.issue_id = i.id AND ipr.close_intent
  )
RETURNING i.*;
