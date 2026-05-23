-- name: ListUnlinkedPullRequestsMatchingIssueIdentifier :many
-- Self-heals missing issue_pull_request rows when a mirrored PR already
-- mentions the issue identifier.
SELECT pr.*
FROM github_pull_request pr
LEFT JOIN issue_pull_request ipr
    ON ipr.issue_id = sqlc.arg('issue_id')
   AND ipr.pull_request_id = pr.id
WHERE pr.workspace_id = sqlc.arg('workspace_id')
  AND ipr.issue_id IS NULL
  AND (
      pr.title ~* sqlc.arg('identifier_regex')
      OR COALESCE(pr.branch, '') ~* sqlc.arg('identifier_regex')
  )
ORDER BY pr.pr_created_at DESC;
