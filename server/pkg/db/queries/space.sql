-- name: ListWorkspaceSpaces :many
SELECT * FROM workspace_space
WHERE workspace_id = $1
ORDER BY is_default DESC, archived_at NULLS FIRST, name ASC, created_at ASC;

-- name: ListActiveWorkspaceSpaces :many
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND archived_at IS NULL
ORDER BY is_default DESC, name ASC, created_at ASC;

-- name: ListWorkspaceSpacesByIDs :many
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND id = ANY(sqlc.arg('space_ids')::uuid[]);

-- name: GetWorkspaceSpace :one
SELECT * FROM workspace_space
WHERE id = $1 AND workspace_id = $2;

-- name: GetDefaultWorkspaceSpace :one
SELECT * FROM workspace_space
WHERE workspace_id = $1 AND is_default
LIMIT 1;

-- name: GetWorkspaceSpaceByKey :one
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND lower(key) = lower($2)
LIMIT 1;

-- name: CreateWorkspaceSpace :one
INSERT INTO workspace_space (
    workspace_id, name, key, description, icon, is_default, created_by
) VALUES (
    $1, $2, $3, COALESCE(sqlc.narg('description')::text, ''), sqlc.narg('icon')::text, $4, sqlc.narg('created_by')
) RETURNING *;

-- name: UpdateWorkspaceSpace :one
UPDATE workspace_space SET
    name = COALESCE(sqlc.narg('name'), name),
    key = COALESCE(sqlc.narg('key'), key),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveWorkspaceSpace :one
UPDATE workspace_space SET
    archived_at = now(),
    archived_by = $3,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND is_default = false
  AND archived_at IS NULL
RETURNING *;

-- name: IncrementSpaceIssueCounter :one
UPDATE workspace_space
SET issue_counter = issue_counter + 1,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND archived_at IS NULL
RETURNING issue_counter;

-- name: LockWorkspaceSpaceForKeyUpdate :one
SELECT * FROM workspace_space
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: AddWorkspaceSpaceMember :exec
INSERT INTO workspace_space_member (workspace_id, space_id, user_id, role, sort_order)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (space_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: NextSpaceMemberSortOrder :one
-- Next slot at the end of this user's space list (per-user ordering).
SELECT (COALESCE(MAX(sort_order), 0) + 1)::double precision FROM workspace_space_member
WHERE workspace_id = $1
  AND user_id = $2;

-- name: GetWorkspaceSpaceMember :one
SELECT * FROM workspace_space_member
WHERE space_id = $1
  AND user_id = $2;

-- name: UpdateSpaceMemberSortOrder :one
UPDATE workspace_space_member
SET sort_order = $4
WHERE workspace_id = $1
  AND space_id = $2
  AND user_id = $3
RETURNING *;

-- name: ListWorkspaceSpacesForUser :many
-- Space list enriched with the requesting user's membership (drives the
-- sidebar Spaces section: only joined spaces, ordered by member sort_order).
SELECT sqlc.embed(wt),
       (m.user_id IS NOT NULL)::boolean AS is_member,
       COALESCE(m.sort_order, 0)::double precision AS member_sort_order
FROM workspace_space wt
LEFT JOIN workspace_space_member m
    ON m.space_id = wt.id AND m.user_id = $2
WHERE wt.workspace_id = $1
ORDER BY wt.is_default DESC, wt.archived_at NULLS FIRST, wt.name ASC, wt.created_at ASC;

-- name: RemoveWorkspaceSpaceMember :execrows
DELETE FROM workspace_space_member
WHERE workspace_id = $1
  AND space_id = $2
  AND user_id = $3;

-- name: ListWorkspaceSpaceMembersWithUser :many
SELECT m.workspace_id, m.space_id, m.user_id, m.role, m.sort_order, m.created_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM workspace_space_member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
  AND m.space_id = $2
ORDER BY m.role ASC, m.created_at ASC;

-- name: ListWorkspaceSpaceMembers :many
SELECT * FROM workspace_space_member
WHERE workspace_id = $1
  AND space_id = $2
ORDER BY role ASC, created_at ASC;
