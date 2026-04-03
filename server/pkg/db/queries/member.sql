-- name: ListMembers :many
SELECT * FROM member
WHERE workspace_id = ?
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM member
WHERE id = ?;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM member
WHERE user_id = ? AND workspace_id = ?;

-- name: CreateMember :one
INSERT INTO member (id, workspace_id, user_id, role)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: UpdateMemberRole :one
UPDATE member SET role = ?
WHERE id = ?
RETURNING *;

-- name: DeleteMember :exec
DELETE FROM member WHERE id = ?;

-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN user u ON u.id = m.user_id
WHERE m.workspace_id = ?
ORDER BY m.created_at ASC;
