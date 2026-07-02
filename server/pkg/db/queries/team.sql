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
INSERT INTO workspace_team_member (workspace_id, team_id, user_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: ListWorkspaceTeamMembers :many
SELECT * FROM workspace_team_member
WHERE workspace_id = $1
  AND team_id = $2
ORDER BY role ASC, created_at ASC;
