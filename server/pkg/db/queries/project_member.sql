-- name: ListProjectMembers :many
SELECT pm.*, u.name, u.email, u.avatar_url
FROM project_member pm
JOIN "user" u ON u.id = pm.user_id
WHERE pm.project_id = $1
ORDER BY pm.added_at ASC;

-- name: AddProjectMember :one
INSERT INTO project_member (workspace_id, project_id, user_id, added_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, user_id) DO UPDATE
SET added_at = project_member.added_at
RETURNING *;

-- name: RemoveProjectMember :exec
DELETE FROM project_member WHERE project_id = $1 AND user_id = $2;

-- name: IsProjectMember :one
SELECT EXISTS (
    SELECT 1 FROM project_member WHERE project_id = $1 AND user_id = $2
) AS is_member;

-- name: ListProjectMembershipsForUser :many
-- Restricted projects this user is an explicit member of, scoped to a workspace.
-- Used by the workspace member detail page (Projects tab).
SELECT p.id, p.title, p.icon, p.color, pm.added_at
FROM project_member pm
JOIN project p ON p.id = pm.project_id
WHERE pm.workspace_id = $1
  AND pm.user_id = $2
  AND p.access = 'restricted'
ORDER BY pm.added_at DESC;

-- name: CountRestrictedProjectsInWorkspace :one
SELECT count(*) FROM project
WHERE workspace_id = $1 AND access = 'restricted';
