-- Capability rows (group can create runtimes / agents).

-- name: ListCerebroGroupCapabilities :many
SELECT group_id, capability, granted_by, granted_at
FROM cerebro_group_capability
WHERE group_id = $1
ORDER BY capability ASC;

-- name: SetCerebroGroupCapability :one
INSERT INTO cerebro_group_capability (group_id, capability, granted_by)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, capability) DO UPDATE
    SET granted_by = EXCLUDED.granted_by,
        granted_at = now()
RETURNING group_id, capability, granted_by, granted_at;

-- name: RemoveCerebroGroupCapability :exec
DELETE FROM cerebro_group_capability
WHERE group_id = $1 AND capability = $2;

-- name: HasCerebroCapability :one
-- True iff any of the user's groups grants the capability. Used by PR 3
-- enforcement; admin override is handled upstream (callers OR-in IsAdmin).
SELECT EXISTS (
    SELECT 1
    FROM cerebro_group_capability c
    JOIN cerebro_group g  ON g.id  = c.group_id
    JOIN cerebro_group_member gm ON gm.group_id = g.id
    WHERE g.workspace_id = $1
      AND gm.user_id     = $2
      AND c.capability   = $3
) AS allowed;

-- Runtime allowlist rows.

-- name: ListCerebroGroupRuntimes :many
SELECT group_id, runtime_id, granted_by, granted_at
FROM cerebro_group_runtime_access
WHERE group_id = $1
ORDER BY granted_at ASC;

-- name: AddCerebroGroupRuntime :one
INSERT INTO cerebro_group_runtime_access (group_id, runtime_id, granted_by)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, runtime_id) DO UPDATE
    SET granted_by = EXCLUDED.granted_by,
        granted_at = now()
RETURNING group_id, runtime_id, granted_by, granted_at;

-- name: RemoveCerebroGroupRuntime :exec
DELETE FROM cerebro_group_runtime_access
WHERE group_id = $1 AND runtime_id = $2;

-- name: HasCerebroRuntimeAccess :one
SELECT EXISTS (
    SELECT 1
    FROM cerebro_group_runtime_access ra
    JOIN cerebro_group_member gm ON gm.group_id = ra.group_id
    WHERE ra.runtime_id = $1
      AND gm.user_id    = $2
) AS allowed;

-- Agent allowlist rows.

-- name: ListCerebroGroupAgents :many
SELECT group_id, agent_id, granted_by, granted_at
FROM cerebro_group_agent_access
WHERE group_id = $1
ORDER BY granted_at ASC;

-- name: AddCerebroGroupAgent :one
INSERT INTO cerebro_group_agent_access (group_id, agent_id, granted_by)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, agent_id) DO UPDATE
    SET granted_by = EXCLUDED.granted_by,
        granted_at = now()
RETURNING group_id, agent_id, granted_by, granted_at;

-- name: RemoveCerebroGroupAgent :exec
DELETE FROM cerebro_group_agent_access
WHERE group_id = $1 AND agent_id = $2;

-- name: HasCerebroAgentAccess :one
SELECT EXISTS (
    SELECT 1
    FROM cerebro_group_agent_access aa
    JOIN cerebro_group_member gm ON gm.group_id = aa.group_id
    WHERE aa.agent_id = $1
      AND gm.user_id  = $2
) AS allowed;

-- Project group-access rows.

-- name: ListCerebroProjectGroups :many
SELECT project_id, group_id, added_by, added_at
FROM cerebro_project_group_member
WHERE project_id = $1
ORDER BY added_at ASC;

-- name: AddCerebroProjectGroup :one
INSERT INTO cerebro_project_group_member (project_id, group_id, added_by)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, group_id) DO UPDATE
    SET added_by = EXCLUDED.added_by,
        added_at = now()
RETURNING project_id, group_id, added_by, added_at;

-- name: RemoveCerebroProjectGroup :exec
DELETE FROM cerebro_project_group_member
WHERE project_id = $1 AND group_id = $2;

-- name: HasCerebroProjectAccessViaGroup :one
-- True iff any of the user's groups has been granted access to the project.
-- The OR-led for existing project_member rows + admin override is layered
-- in the Go-side helper.
SELECT EXISTS (
    SELECT 1
    FROM cerebro_project_group_member pgm
    JOIN cerebro_group_member gm ON gm.group_id = pgm.group_id
    WHERE pgm.project_id = $1
      AND gm.user_id     = $2
) AS allowed;

-- Default group helpers.
--
-- The workspace -> default-group pointer lives in cerebro_workspace_default_group
-- (separate row so cerebro_group's column list stays untouched). Callers should
-- tolerate the not-found case (pgx.ErrNoRows) — it means the workspace was
-- created before the backfill or its default row was manually removed.

-- name: GetCerebroDefaultGroupID :one
SELECT group_id
FROM cerebro_workspace_default_group
WHERE workspace_id = $1;

-- name: IsCerebroDefaultGroup :one
SELECT EXISTS (
    SELECT 1
    FROM cerebro_workspace_default_group
    WHERE group_id = $1
) AS is_default;

-- name: SetCerebroDefaultGroup :exec
INSERT INTO cerebro_workspace_default_group (workspace_id, group_id)
VALUES ($1, $2)
ON CONFLICT (workspace_id) DO UPDATE
    SET group_id = EXCLUDED.group_id;

-- ListCerebroGroupIDsForUser returns the IDs of every group the user belongs
-- to in the workspace. Used by the resolver to build the Viewer.GroupIDs slice
-- in one round-trip per request.

-- name: ListCerebroGroupIDsForUser :many
SELECT g.id
FROM cerebro_group g
JOIN cerebro_group_member gm ON gm.group_id = g.id
WHERE g.workspace_id = $1
  AND gm.user_id     = $2;

-- ListCerebroAgentIDsForUser, ListCerebroRuntimeIDsForUser and
-- ListCerebroProjectIDsForUser return the resource IDs the viewer is granted
-- access to via any group membership in the workspace. PR 4 of JEH-1006 uses
-- these to filter the corresponding list endpoints — admin override is layered
-- on top in Go, so the SQL stays simple.
--
-- Each query joins through cerebro_group to enforce workspace scope (a group
-- is workspace-bound, so a grant from outside the workspace cannot leak in).

-- name: ListCerebroAgentIDsForUser :many
SELECT DISTINCT aa.agent_id
FROM cerebro_group_agent_access aa
JOIN cerebro_group g          ON g.id        = aa.group_id
JOIN cerebro_group_member gm  ON gm.group_id = aa.group_id
WHERE g.workspace_id = $1
  AND gm.user_id     = $2;

-- name: ListCerebroRuntimeIDsForUser :many
SELECT DISTINCT ra.runtime_id
FROM cerebro_group_runtime_access ra
JOIN cerebro_group g          ON g.id        = ra.group_id
JOIN cerebro_group_member gm  ON gm.group_id = ra.group_id
WHERE g.workspace_id = $1
  AND gm.user_id     = $2;

-- name: ListCerebroProjectIDsForUser :many
SELECT DISTINCT pgm.project_id
FROM cerebro_project_group_member pgm
JOIN cerebro_group g          ON g.id        = pgm.group_id
JOIN cerebro_group_member gm  ON gm.group_id = pgm.group_id
WHERE g.workspace_id = $1
  AND gm.user_id     = $2;

-- ListCerebroUserIDsForProject — the inverse of ListCerebroProjectIDsForUser:
-- returns every member with group-level access to the project. Used by the WS
-- audience builder so realtime events also reach group-only viewers (otherwise
-- a member would only see updates on a refresh).

-- name: ListCerebroUserIDsForProject :many
SELECT DISTINCT gm.user_id
FROM cerebro_project_group_member pgm
JOIN cerebro_group_member gm ON gm.group_id = pgm.group_id
WHERE pgm.project_id = $1;
