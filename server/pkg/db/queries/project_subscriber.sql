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
-- Members watching the project this issue belongs to. A member who left this
-- issue, or a tree containing it (the same opt-out rule as HasAncestorOptOut),
-- is not brought back through the project: leaving an issue is the narrower,
-- more recent intent. Private-project visibility is enforced by
-- CreateInboxItem, not here.
WITH RECURSIVE ancestors(node_id, parent_id, depth) AS (
    SELECT root.id, root.parent_issue_id, 0 FROM issue root WHERE root.id = sqlc.arg('issue_id')
    UNION ALL
    SELECT i.id, i.parent_issue_id, a.depth + 1 FROM issue i JOIN ancestors a ON i.id = a.parent_id
)
SELECT ps.user_id
FROM issue target
JOIN project_subscriber ps
  ON ps.project_id = target.project_id AND ps.workspace_id = target.workspace_id
WHERE target.id = sqlc.arg('issue_id')
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
