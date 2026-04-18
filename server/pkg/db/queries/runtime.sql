-- name: ListAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgentRuntime :one
SELECT * FROM agent_runtime
WHERE id = $1;

-- name: GetAgentRuntimeForWorkspace :one
SELECT * FROM agent_runtime
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertAgentRuntime :one
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    owner_id,
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (workspace_id, daemon_id, provider)
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    last_seen_at = now(),
    updated_at = now()
RETURNING *;

-- name: UpdateAgentRuntimeHeartbeat :one
UPDATE agent_runtime
SET status = 'online', last_seen_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: MarkStaleRuntimesOffline :many
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE status = 'online'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
RETURNING id, workspace_id;

-- name: FailTasksForOfflineRuntimes :many
-- Marks dispatched/running tasks as failed when their runtime is offline.
-- This cleans up orphaned tasks after a daemon crash or network partition.
UPDATE agent_task_queue
SET status = 'failed', completed_at = now(), error = 'runtime went offline'
WHERE status IN ('dispatched', 'running')
  AND runtime_id IN (
    SELECT id FROM agent_runtime WHERE status = 'offline'
  )
RETURNING id, agent_id, issue_id;

-- name: ListAgentRuntimesByOwner :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1 AND owner_id = $2
ORDER BY created_at ASC;

-- name: DeleteAgentRuntime :exec
DELETE FROM agent_runtime WHERE id = $1;

-- name: CountActiveAgentsByRuntime :one
SELECT count(*) FROM agent WHERE runtime_id = $1 AND archived_at IS NULL;

-- name: DeleteArchivedAgentsByRuntime :exec
DELETE FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL;

-- name: FindLegacyRuntimesByDaemonID :many
-- Looks up runtime rows keyed on a prior (hostname-derived) daemon_id. Used
-- at register-time to find rows owned by the same machine under its old
-- identity so agents/tasks can be re-pointed at the new UUID-keyed row.
--
-- Comparison is case-insensitive because os.Hostname() has been observed to
-- return different casings on the same machine (e.g. `Jiayuans-MacBook-Pro`
-- vs `jiayuans-macbook-pro`) across reboots/mDNS state changes. A case-
-- sensitive `=` would strand the old row; LOWER() on both sides handles drift
-- without forcing the daemon to enumerate cased permutations.
--
-- Returns many rather than one because case drift may have already minted
-- duplicate rows historically (e.g. `Foo.local` AND `foo.local` under the
-- same workspace+provider). A single-row lookup would consolidate only one
-- of them and leave the rest orphaned. Callers must merge every returned
-- row into the new UUID-keyed runtime.
SELECT * FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND provider = @provider
  AND LOWER(daemon_id) = LOWER(@daemon_id);

-- name: ReassignAgentsToRuntime :execrows
-- Re-points every agent referencing old_runtime_id at new_runtime_id.
UPDATE agent
SET runtime_id = @new_runtime_id
WHERE runtime_id = @old_runtime_id;

-- name: ReassignTasksToRuntime :execrows
-- Re-points every queued/running/completed task referencing old_runtime_id.
-- Required before deleting the old runtime row because agent_task_queue has
-- an ON DELETE CASCADE FK that would otherwise drop historical tasks.
UPDATE agent_task_queue
SET runtime_id = @new_runtime_id
WHERE runtime_id = @old_runtime_id;

-- name: RecordRuntimeLegacyDaemonID :exec
-- Remembers the most recent hostname-derived daemon_id that was merged into
-- this row. Useful for debugging when tracing back why a given runtime row
-- subsumed an old one, and only overwrites NULL so the earliest merge is
-- preserved.
UPDATE agent_runtime
SET legacy_daemon_id = COALESCE(legacy_daemon_id, $2)
WHERE id = $1;

-- name: DeleteStaleOfflineRuntimes :many
-- Deletes runtimes that have been offline for longer than the TTL and have
-- no agents bound (active or archived). The FK constraint on agent.runtime_id
-- is ON DELETE RESTRICT, so we must exclude all agent references.
DELETE FROM agent_runtime
WHERE status = 'offline'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
  AND id NOT IN (SELECT DISTINCT runtime_id FROM agent)
RETURNING id, workspace_id;
