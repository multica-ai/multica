-- name: ListCerebroGroups :many
SELECT id, workspace_id, name, description, created_by, created_at, updated_at
FROM cerebro_group
WHERE workspace_id = $1
ORDER BY lower(name), created_at ASC;

-- name: GetCerebroGroup :one
SELECT id, workspace_id, name, description, created_by, created_at, updated_at
FROM cerebro_group
WHERE id = $1;

-- name: CreateCerebroGroup :one
INSERT INTO cerebro_group (workspace_id, name, description, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id, workspace_id, name, description, created_by, created_at, updated_at;

-- name: UpdateCerebroGroup :one
UPDATE cerebro_group
SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING id, workspace_id, name, description, created_by, created_at, updated_at;

-- name: DeleteCerebroGroup :exec
DELETE FROM cerebro_group
WHERE id = $1;

-- name: AddCerebroGroupMember :one
WITH inserted AS (
    INSERT INTO cerebro_group_member (group_id, user_id, added_by)
    VALUES ($1, $2, $3)
    ON CONFLICT (group_id, user_id) DO NOTHING
    RETURNING group_id, user_id, added_by, added_at
),
selected AS (
    SELECT group_id, user_id, added_by, added_at FROM inserted
    UNION ALL
    SELECT group_id, user_id, added_by, added_at
    FROM cerebro_group_member
    WHERE group_id = $1
      AND user_id = $2
      AND NOT EXISTS (SELECT 1 FROM inserted)
)
SELECT s.group_id, s.user_id, s.added_by, s.added_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM selected s
JOIN "user" u ON u.id = s.user_id
LIMIT 1;

-- name: RemoveCerebroGroupMember :one
WITH deleted AS (
    DELETE FROM cerebro_group_member
    WHERE group_id = $1 AND user_id = $2
    RETURNING group_id, user_id, added_by, added_at
)
SELECT d.group_id, d.user_id, d.added_by, d.added_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM deleted d
JOIN "user" u ON u.id = d.user_id;

-- name: ListCerebroGroupMembers :many
SELECT gm.group_id, gm.user_id, gm.added_by, gm.added_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM cerebro_group_member gm
JOIN "user" u ON u.id = gm.user_id
WHERE gm.group_id = $1
ORDER BY u.name ASC, gm.added_at ASC;

-- name: ListCerebroGroupsForUser :many
SELECT g.id, g.workspace_id, g.name, g.description, g.created_by, g.created_at, g.updated_at
FROM cerebro_group g
JOIN cerebro_group_member gm ON gm.group_id = g.id
WHERE g.workspace_id = $1
  AND gm.user_id = $2
ORDER BY lower(g.name), g.created_at ASC;
