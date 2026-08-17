-- Custom Runtime profiles (MUL-3284). Workspace-level definitions of a custom
-- runtime; see migration 120 for the table. Relational integrity (workspace,
-- created_by) is enforced in the application layer — there are no DB FKs.

-- name: CreateRuntimeProfile :one
INSERT INTO runtime_profile (
    workspace_id,
    display_name,
    protocol_family,
    command_name,
    description,
    fixed_args,
    visibility,
    activation_mode,
    created_by,
    enabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at;

-- name: GetRuntimeProfile :one
SELECT id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at FROM runtime_profile
WHERE id = $1;

-- name: GetRuntimeProfileForWorkspace :one
SELECT id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at FROM runtime_profile
WHERE id = $1 AND workspace_id = $2;

-- name: LockRuntimeProfileForRegistration :one
-- Serializes daemon registration with profile deletion. Registration holds a
-- KEY SHARE lock until its runtime row is committed; profile deletion takes an
-- UPDATE lock, then locks the profile's runtime rows. Whichever starts first
-- wins, so deletion cannot miss a runtime inserted from a stale profile read.
SELECT id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at FROM runtime_profile
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockRuntimeProfileForDelete :one
-- See LockRuntimeProfileForRegistration. The stronger lock prevents a daemon
-- from registering another instance between the delete plan and commit.
SELECT id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at FROM runtime_profile
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: ListRuntimeProfiles :many
SELECT id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at FROM runtime_profile
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListEnabledRuntimeProfilesForWorkspace :many
-- Daemon-facing list: only enabled profiles are candidates for a daemon to
-- resolve on PATH and register. Ordered for stable output.
SELECT id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at FROM runtime_profile
WHERE workspace_id = $1 AND enabled = true
ORDER BY created_at ASC;

-- name: UpdateRuntimeProfile :one
-- Partial update via COALESCE: NULL args leave the column unchanged. The
-- protocol_family is intentionally NOT updatable — changing the underlying
-- backend of an existing profile would silently repoint every agent bound to
-- it onto a different protocol; callers create a new profile instead.
UPDATE runtime_profile
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    command_name = COALESCE(sqlc.narg('command_name'), command_name),
    description  = COALESCE(sqlc.narg('description'), description),
    fixed_args   = COALESCE(sqlc.narg('fixed_args'), fixed_args),
    visibility   = COALESCE(sqlc.narg('visibility'), visibility),
    activation_mode = COALESCE(sqlc.narg('activation_mode'), activation_mode),
    enabled      = COALESCE(sqlc.narg('enabled'), enabled),
    updated_at   = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING id, workspace_id, display_name, protocol_family, command_name, description, fixed_args, visibility, activation_mode, created_by, enabled, created_at, updated_at;

-- name: DeleteRuntimeProfile :exec
DELETE FROM runtime_profile
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteAgentRuntimesByProfile :many
-- Application-layer cascade: migration 120 dropped the DB ON DELETE CASCADE, so
-- the profile-delete path must remove the profile's registered runtime
-- instances itself. Returns the deleted rows so the caller can broadcast /
-- audit. Runs inside the same transaction as DeleteRuntimeProfile.
DELETE FROM agent_runtime
WHERE profile_id = $1 AND workspace_id = $2
RETURNING id, workspace_id, owner_id, daemon_id, provider;

-- name: CountAgentsByProfile :one
-- Counts active, user-visible agents bound to any runtime instance of this
-- profile. Hidden system agents are teardown-owned infrastructure and must not
-- block the code path that deletes them.
SELECT count(*) FROM agent a
JOIN agent_runtime ar ON ar.id = a.runtime_id
WHERE ar.profile_id = $1
  AND ar.workspace_id = $2
  AND a.archived_at IS NULL
  AND a.kind = 'user';

-- name: ListAgentRuntimeIDsByProfile :many
-- Enumerates the runtime instance rows registered against a profile. The
-- profile-delete cascade walks these so it can run the same archived-agent /
-- archived-squad / autopilot teardown the runtime-delete path uses before
-- removing each runtime row — agent.runtime_id is ON DELETE RESTRICT, so a
-- bare delete would 500 whenever an archived agent still references the row.
SELECT id FROM agent_runtime
WHERE profile_id = $1 AND workspace_id = $2
ORDER BY id
FOR UPDATE;
