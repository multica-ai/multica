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
    created_by,
    enabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetRuntimeProfile :one
SELECT * FROM runtime_profile
WHERE id = $1;

-- name: GetRuntimeProfileForWorkspace :one
SELECT * FROM runtime_profile
WHERE id = $1 AND workspace_id = $2;

-- name: LockRuntimeProfileForRegistration :one
-- Serializes daemon registration with profile deletion. Registration holds a
-- KEY SHARE lock until its runtime row is committed; profile deletion takes an
-- UPDATE lock, then locks the profile's runtime rows. Whichever starts first
-- wins, so deletion cannot miss a runtime inserted from a stale profile read.
SELECT * FROM runtime_profile
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockRuntimeProfileForDelete :one
-- See LockRuntimeProfileForRegistration. The stronger lock prevents a daemon
-- from registering another instance between the delete plan and commit.
SELECT * FROM runtime_profile
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: ListRuntimeProfiles :many
SELECT * FROM runtime_profile
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListEnabledRuntimeProfilesForWorkspace :many
-- Daemon-facing list: only enabled profiles are candidates for a daemon to
-- resolve on PATH and register. Ordered for stable output.
SELECT * FROM runtime_profile
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
    enabled      = COALESCE(sqlc.narg('enabled'), enabled),
    updated_at   = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

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

-- name: ListActiveAgentsByProfile :many
-- Active (non-archived) agents bound to any runtime instance of this profile.
-- The profile-delete path uses this to refuse deletion (409) while agents
-- still depend on it, mirroring the runtime-delete guard.
--
-- It returns the rows rather than a count because the refusal has to name
-- them: a profile spans every machine that registered it, so the agents
-- blocking the delete are routinely bound to a different machine than the one
-- the user was trying to clean up, and a bare count gives them no way to tell
-- (GH #8456). Carrying the runtime is what lets the message say which machine.
--
-- Deliberately not filtered by kind: it defines when deletion is refused, and
-- narrowing it to user agents here would let a profile with a bound builder
-- carrier through, whereupon TeardownRuntime would hard-delete that carrier.
--
-- system_key rides along because it, not kind, decides what the user can
-- actually do about a blocker. Mika is kind='user' with system_key='mika' and
-- can be neither archived nor moved; a builder carrier is kind='system' and is
-- released by its Builder session, not from the agent list.
--
-- Bounded on purpose. The caller only needs to know that blockers exist, name a
-- few, and report how many there are — it never needs every row. This runs
-- inside the delete transaction while profile, runtime and agent rows are
-- locked, and the response is buffered whole before it is written, so an
-- unbounded read here would make both the time under lock and the response body
-- grow with the number of agents a profile has accumulated across machines.
--
-- The per-class counts are what let the refusal stay correct while bounded.
-- Rows are ordered by machine and name, so which blockers land inside the LIMIT
-- is arbitrary with respect to class: twenty ordinary agents can push the one
-- Mika to position 21. A message whose advice came from the visible rows would
-- then tell the user to archive all of them — the unactionable instruction this
-- whole change removes, just deferred. So the recovery paths are chosen from
-- these counts and only the names come from the rows.
--
-- Every count is a window function over the pre-LIMIT result, so they stay
-- exact no matter how small max_rows is. blocker_class is emitted per row as
-- well, giving the classification one definition that the Go classifier is
-- pinned against in tests.
WITH blockers AS (
    SELECT
        a.id,
        a.name,
        a.kind,
        a.system_key,
        ar.id AS runtime_id,
        ar.name AS runtime_name,
        ar.custom_name AS runtime_custom_name,
        ar.status AS runtime_status,
        CASE
            WHEN a.system_key IS NULL OR btrim(a.system_key) = '' THEN 'user'
            WHEN btrim(a.system_key) = 'mika' THEN 'mika'
            -- starts_with, not LIKE: '_' is a single-character wildcard in
            -- LIKE, so 'agent_builder:%' also matches 'agent-builder:x' and
            -- 'agentXbuilder:x'. Those would be classified here as carriers
            -- and by the Go side as other_system, and the refusal would send
            -- the user to an Agent Builder session that does not exist.
            WHEN starts_with(btrim(a.system_key), 'agent_builder:') THEN 'agent_builder'
            ELSE 'other_system'
        END AS blocker_class
    FROM agent a
    JOIN agent_runtime ar ON ar.id = a.runtime_id
    WHERE ar.profile_id = $1 AND ar.workspace_id = $2 AND a.archived_at IS NULL
)
SELECT
    id,
    name,
    kind,
    system_key,
    runtime_id,
    runtime_name,
    runtime_custom_name,
    runtime_status,
    blocker_class,
    count(*) OVER () AS total_count,
    count(*) FILTER (WHERE blocker_class = 'user') OVER () AS user_count,
    count(*) FILTER (WHERE blocker_class = 'mika') OVER () AS mika_count,
    count(*) FILTER (WHERE blocker_class = 'agent_builder') OVER () AS agent_builder_count,
    count(*) FILTER (WHERE blocker_class = 'other_system') OVER () AS other_system_count
FROM blockers
ORDER BY runtime_name ASC, name ASC
LIMIT @max_rows::int;

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
