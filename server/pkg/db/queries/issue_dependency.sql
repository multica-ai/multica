-- name: ListIssuesDependingOn :many
SELECT i.*
FROM issue i
JOIN issue_dependency d ON d.issue_id = i.id
JOIN issue upstream ON upstream.id = d.depends_on_issue_id
WHERE d.depends_on_issue_id = $1
  AND d.type IN ('blocks', 'blocked_by')
  AND i.workspace_id = upstream.workspace_id
ORDER BY i.position ASC, i.created_at DESC;

-- name: CountUnresolvedIssueDependencies :one
SELECT COUNT(*)::bigint
FROM issue dependent
JOIN issue_dependency d ON d.issue_id = dependent.id
JOIN issue upstream ON upstream.id = d.depends_on_issue_id
WHERE dependent.id = $1
  AND d.type IN ('blocks', 'blocked_by')
  AND upstream.workspace_id = dependent.workspace_id
  AND upstream.status != 'done';

