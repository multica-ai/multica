-- name: ListWorkspaceSpaces :many
SELECT * FROM workspace_space
WHERE workspace_id = $1
ORDER BY archived_at NULLS FIRST, name ASC, created_at ASC;

-- name: ListActiveWorkspaceSpaces :many
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND archived_at IS NULL
ORDER BY name ASC, created_at ASC;

-- name: ListActiveWorkspaceSpacesForUpdate :many
-- Locks every active Space row for this workspace so a concurrent archive on
-- another Space in the same workspace serializes behind this one. Used by
-- ArchiveSpace to count active Spaces before archiving without racing another
-- archive down to zero.
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND archived_at IS NULL
FOR UPDATE;

-- name: ListWorkspaceSpacesByIDs :many
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND id = ANY(sqlc.arg('space_ids')::uuid[]);

-- name: GetWorkspaceSpace :one
SELECT * FROM workspace_space
WHERE id = $1 AND workspace_id = $2;

-- name: GetDefaultWorkspaceSpace :one
-- Stable workspace-level fallback for context-free creation and imports.
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND is_default = true
  AND archived_at IS NULL
LIMIT 1;

-- name: ClearDefaultWorkspaceSpace :exec
UPDATE workspace_space
SET is_default = false,
    updated_at = now()
WHERE workspace_id = $1
  AND is_default = true;

-- name: SetDefaultWorkspaceSpace :one
UPDATE workspace_space
SET is_default = true,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND archived_at IS NULL
RETURNING *;

-- name: GetWorkspaceSpaceByKey :one
SELECT * FROM workspace_space
WHERE workspace_id = $1
  AND lower(key) = lower($2)
LIMIT 1;

-- name: CreateWorkspaceSpace :one
INSERT INTO workspace_space (
    workspace_id, name, key, icon, visibility, created_by
) VALUES (
    $1, $2, $3, sqlc.narg('icon')::text,
    COALESCE(sqlc.narg('visibility')::text, 'open'), sqlc.narg('created_by')
) RETURNING *;

-- name: UpdateWorkspaceSpace :one
UPDATE workspace_space SET
    name = COALESCE(sqlc.narg('name'), name),
    key = COALESCE(sqlc.narg('key'), key),
    icon = COALESCE(sqlc.narg('icon'), icon),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveWorkspaceSpace :one
-- Callers must first lock the workspace's active Spaces (see
-- ListActiveWorkspaceSpacesForUpdate) and confirm more than one remains —
-- this query no longer guards that itself.
UPDATE workspace_space SET
    archived_at = now(),
    archived_by = $3,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
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

-- name: UpdateWorkspaceSpaceMemberRole :one
UPDATE workspace_space_member
SET role = $4
WHERE workspace_id = $1
  AND space_id = $2
  AND user_id = $3
RETURNING *;

-- name: ListWorkspaceSpacesForUser :many
-- Space list enriched with the requesting user's membership and personal
-- preferences. Clients decide whether to show all discoverable Spaces or the
-- joined/pinned subset used by the Sidebar.
SELECT sqlc.embed(wt),
       (m.user_id IS NOT NULL)::boolean AS is_member,
       m.role AS member_role,
       COALESCE(pref.is_pinned, false)::boolean AS is_pinned,
       COALESCE(pref.is_followed, false)::boolean AS is_followed,
       COALESCE(pref.sort_order, m.sort_order, 0)::double precision AS member_sort_order
FROM workspace_space wt
LEFT JOIN workspace_space_member m
    ON m.space_id = wt.id AND m.user_id = $2
LEFT JOIN workspace_space_preference pref
    ON pref.space_id = wt.id AND pref.user_id = $2
JOIN member wm
    ON wm.workspace_id = wt.workspace_id AND wm.user_id = $2
WHERE wt.workspace_id = $1
  AND (wt.visibility = 'open' OR m.user_id IS NOT NULL OR wm.role IN ('owner', 'admin'))
ORDER BY wt.archived_at NULLS FIRST, wt.name ASC, wt.created_at ASC;

-- name: GetWorkspaceSpacePreference :one
SELECT * FROM workspace_space_preference
WHERE workspace_id = $1
  AND space_id = $2
  AND user_id = $3;

-- name: UpsertWorkspaceSpacePreference :one
INSERT INTO workspace_space_preference (
    workspace_id, space_id, user_id, is_pinned, is_followed, sort_order
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (space_id, user_id) DO UPDATE SET
    is_pinned = EXCLUDED.is_pinned,
    is_followed = EXCLUDED.is_followed,
    sort_order = EXCLUDED.sort_order,
    updated_at = now()
RETURNING *;

-- name: NextSpacePreferenceSortOrder :one
SELECT (COALESCE(MAX(sort_order), 0) + 1)::double precision
FROM (
    SELECT pref.sort_order
    FROM workspace_space_preference pref
    WHERE pref.workspace_id = sqlc.arg('workspace_id')
      AND pref.user_id = sqlc.arg('user_id')
    UNION ALL
    SELECT membership.sort_order
    FROM workspace_space_member membership
    WHERE membership.workspace_id = sqlc.arg('workspace_id')
      AND membership.user_id = sqlc.arg('user_id')
) personal_space_order;

-- name: CanViewWorkspaceSpace :one
SELECT EXISTS (
    SELECT 1
    FROM workspace_space wt
    JOIN member wm
      ON wm.workspace_id = wt.workspace_id AND wm.user_id = $3
    LEFT JOIN workspace_space_member sm
      ON sm.space_id = wt.id AND sm.user_id = $3
    WHERE wt.workspace_id = $1
      AND wt.id = $2
      AND (wt.visibility = 'open' OR sm.user_id IS NOT NULL OR wm.role IN ('owner', 'admin'))
)::boolean;

-- name: CanCollaborateInWorkspaceSpace :one
SELECT EXISTS (
    SELECT 1
    FROM workspace_space wt
    JOIN member wm
      ON wm.workspace_id = wt.workspace_id AND wm.user_id = $3
    LEFT JOIN workspace_space_member sm
      ON sm.space_id = wt.id AND sm.user_id = $3
    WHERE wt.workspace_id = $1
      AND wt.id = $2
      AND wt.archived_at IS NULL
      AND (wm.role IN ('owner', 'admin') OR sm.role IN ('lead', 'admin', 'member'))
)::boolean;

-- name: CanManageWorkspaceSpace :one
SELECT EXISTS (
    SELECT 1
    FROM workspace_space wt
    JOIN member wm
      ON wm.workspace_id = wt.workspace_id AND wm.user_id = $3
    LEFT JOIN workspace_space_member sm
      ON sm.space_id = wt.id AND sm.user_id = $3
    WHERE wt.workspace_id = $1
      AND wt.id = $2
      AND (wm.role IN ('owner', 'admin') OR sm.role IN ('lead', 'admin'))
)::boolean;

-- name: CountWorkspaceSpaceManagers :one
SELECT count(*) FROM workspace_space_member
WHERE workspace_id = $1
  AND space_id = $2
  AND role IN ('lead', 'admin');

-- name: ListPrivateWorkspaceSpaceAudienceUserIDs :many
SELECT DISTINCT wm.user_id
FROM member wm
LEFT JOIN workspace_space_member sm
  ON sm.workspace_id = wm.workspace_id
 AND sm.user_id = wm.user_id
 AND sm.space_id = $2
WHERE wm.workspace_id = $1
  AND (wm.role IN ('owner', 'admin') OR sm.user_id IS NOT NULL)
ORDER BY wm.user_id;

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
