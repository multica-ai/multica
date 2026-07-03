-- name: ListWorkspaceTeams :many
SELECT * FROM workspace_team
WHERE workspace_id = $1
ORDER BY is_default DESC, archived_at NULLS FIRST, name ASC, created_at ASC;

-- name: ListActiveWorkspaceTeams :many
SELECT * FROM workspace_team
WHERE workspace_id = $1
  AND archived_at IS NULL
ORDER BY is_default DESC, name ASC, created_at ASC;

-- name: ListWorkspaceTeamsByIDs :many
SELECT * FROM workspace_team
WHERE workspace_id = $1
  AND id = ANY(sqlc.arg('team_ids')::uuid[]);

-- name: GetWorkspaceTeam :one
SELECT * FROM workspace_team
WHERE id = $1 AND workspace_id = $2;

-- name: GetDefaultWorkspaceTeam :one
SELECT * FROM workspace_team
WHERE workspace_id = $1 AND is_default
LIMIT 1;

-- name: GetWorkspaceTeamByKey :one
SELECT * FROM workspace_team
WHERE workspace_id = $1
  AND lower(key) = lower($2)
LIMIT 1;

-- name: CreateWorkspaceTeam :one
INSERT INTO workspace_team (
    workspace_id, name, key, description, icon, is_default, created_by
) VALUES (
    $1, $2, $3, COALESCE(sqlc.narg('description')::text, ''), sqlc.narg('icon')::text, $4, sqlc.narg('created_by')
) RETURNING *;

-- name: UpdateWorkspaceTeam :one
UPDATE workspace_team SET
    name = COALESCE(sqlc.narg('name'), name),
    key = COALESCE(sqlc.narg('key'), key),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveWorkspaceTeam :one
UPDATE workspace_team SET
    archived_at = now(),
    archived_by = $3,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND is_default = false
  AND archived_at IS NULL
RETURNING *;

-- name: IncrementTeamIssueCounter :one
UPDATE workspace_team
SET issue_counter = issue_counter + 1,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND archived_at IS NULL
RETURNING issue_counter;

-- name: LockWorkspaceTeamForKeyUpdate :one
SELECT * FROM workspace_team
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: AddWorkspaceTeamMember :exec
INSERT INTO workspace_team_member (workspace_id, team_id, user_id, role, sort_order)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: NextTeamMemberSortOrder :one
-- Next slot at the end of this user's team list (per-user ordering).
SELECT (COALESCE(MAX(sort_order), 0) + 1)::double precision FROM workspace_team_member
WHERE workspace_id = $1
  AND user_id = $2;

-- name: GetWorkspaceTeamMember :one
SELECT * FROM workspace_team_member
WHERE team_id = $1
  AND user_id = $2;

-- name: UpdateTeamMemberSortOrder :one
UPDATE workspace_team_member
SET sort_order = $4
WHERE workspace_id = $1
  AND team_id = $2
  AND user_id = $3
RETURNING *;

-- name: ListWorkspaceTeamsForUser :many
-- Team list enriched with the requesting user's membership (drives the
-- sidebar Teams section: only joined teams, ordered by member sort_order).
SELECT sqlc.embed(wt),
       (m.user_id IS NOT NULL)::boolean AS is_member,
       COALESCE(m.sort_order, 0)::double precision AS member_sort_order
FROM workspace_team wt
LEFT JOIN workspace_team_member m
    ON m.team_id = wt.id AND m.user_id = $2
WHERE wt.workspace_id = $1
ORDER BY wt.is_default DESC, wt.archived_at NULLS FIRST, wt.name ASC, wt.created_at ASC;

-- name: RemoveWorkspaceTeamMember :execrows
DELETE FROM workspace_team_member
WHERE workspace_id = $1
  AND team_id = $2
  AND user_id = $3;

-- name: ListWorkspaceTeamMembersWithUser :many
SELECT m.workspace_id, m.team_id, m.user_id, m.role, m.sort_order, m.created_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM workspace_team_member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
  AND m.team_id = $2
ORDER BY m.role ASC, m.created_at ASC;

-- name: ListWorkspaceTeamMembers :many
SELECT * FROM workspace_team_member
WHERE workspace_id = $1
  AND team_id = $2
ORDER BY role ASC, created_at ASC;
