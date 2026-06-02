-- name: ListMembers :many
SELECT * FROM multica_member
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM multica_member
WHERE id = $1;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM multica_member
WHERE user_id = $1 AND workspace_id = $2;

-- name: CreateMember :one
INSERT INTO multica_member (workspace_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateMemberRole :one
UPDATE multica_member SET role = $2
WHERE id = $1
RETURNING *;

-- name: DeleteMember :exec
DELETE FROM multica_member WHERE id = $1;

-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM multica_member m
JOIN multica_user u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;
