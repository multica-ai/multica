-- name: ListProjectMembers :many
SELECT pm.project_id, pm.member_id, pm.role, pm.invited_at, pm.invited_by,
       u.email, u.name, u.avatar_url
FROM project_member pm
JOIN member m ON pm.member_id = m.id
JOIN "user" u ON m.user_id = u.id
WHERE pm.project_id = $1
ORDER BY pm.invited_at DESC;

-- name: AddProjectMember :one
INSERT INTO project_member (project_id, member_id, role, invited_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateProjectMemberRole :one
UPDATE project_member
SET role = $3
WHERE project_id = $1 AND member_id = $2
RETURNING *;

-- name: RemoveProjectMember :exec
DELETE FROM project_member
WHERE project_id = $1 AND member_id = $2;
