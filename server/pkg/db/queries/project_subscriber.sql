-- name: AddProjectSubscriber :exec
-- Only a current workspace member can watch a project.
INSERT INTO project_subscriber (workspace_id, project_id, user_id)
SELECT m.workspace_id, sqlc.arg('project_id')::uuid, m.user_id
FROM member m
WHERE m.workspace_id = sqlc.arg('workspace_id') AND m.user_id = sqlc.arg('user_id')
ON CONFLICT (project_id, user_id) DO NOTHING;

-- name: RemoveProjectSubscriber :exec
DELETE FROM project_subscriber
WHERE project_id = sqlc.arg('project_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND user_id = sqlc.arg('user_id');

-- name: IsProjectSubscriber :one
SELECT EXISTS(
    SELECT 1 FROM project_subscriber
    WHERE project_id = sqlc.arg('project_id')
      AND workspace_id = sqlc.arg('workspace_id')
      AND user_id = sqlc.arg('user_id')
) AS subscribed;

-- name: ListProjectSubscribersForIssue :many
-- Members whose project subscription currently covers this issue: still in
-- the workspace, able to see the project, and not opted out of this issue or
-- of a tree containing it (the same rule as HasAncestorOptOut). Leaving an
-- issue is the narrower, more recent intent, so the project never brings the
-- member back. Both notification fanout and the issue's effective subscriber
-- list read this, so the two cannot disagree.
WITH RECURSIVE ancestors(node_id, parent_id, depth) AS (
    SELECT root.id, root.parent_issue_id, 0 FROM issue root WHERE root.id = sqlc.arg('issue_id')
    UNION ALL
    SELECT i.id, i.parent_issue_id, a.depth + 1 FROM issue i JOIN ancestors a ON i.id = a.parent_id
)
SELECT ps.user_id, ps.created_at
FROM issue target
JOIN project p ON p.id = target.project_id AND p.workspace_id = target.workspace_id
JOIN project_subscriber ps
  ON ps.project_id = p.id AND ps.workspace_id = p.workspace_id
JOIN member m ON m.workspace_id = ps.workspace_id AND m.user_id = ps.user_id
WHERE target.id = sqlc.arg('issue_id')
  AND (
      p.visibility <> 'private'
      OR m.role IN ('owner', 'admin')
      OR EXISTS (
          SELECT 1 FROM project_member pm
          WHERE pm.project_id = p.id AND pm.user_id = ps.user_id
      )
  )
  AND NOT EXISTS (
      SELECT 1
      FROM issue_subscriber s
      JOIN ancestors a ON a.node_id = s.issue_id
      WHERE s.user_type = 'member' AND s.user_id = ps.user_id
        AND s.unsubscribed_at IS NOT NULL
        AND (a.depth = 0 OR s.opt_out_scope = 'subtree')
  )
ORDER BY ps.created_at, ps.user_id;

-- name: DeleteProjectSubscribersByProject :exec
DELETE FROM project_subscriber
WHERE project_id = sqlc.arg('project_id') AND workspace_id = sqlc.arg('workspace_id');

-- name: DeleteProjectSubscribersByUser :exec
-- A member leaving the workspace stops watching every project in it.
DELETE FROM project_subscriber
WHERE workspace_id = sqlc.arg('workspace_id') AND user_id = sqlc.arg('user_id');
