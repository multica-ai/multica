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
-- (xmax = 0) AS inserted distinguishes a fresh insert (true) from an upsert
-- that updated an existing row (false). Analytics reads this to fire
-- runtime_registered/runtime_ready only on first-time registration.
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
RETURNING *, (xmax = 0) AS inserted;

-- name: TouchAgentRuntimeLastSeen :execrows
-- Bumps last_seen_at on an already-online runtime. Deliberately does NOT
-- touch status or updated_at: status is unchanged on the hot heartbeat path,
-- and avoiding updated_at keeps the row HOT-eligible (no index columns
-- change) and avoids invalidating any downstream consumer that watches
-- updated_at.
--
-- The status='online' predicate is load-bearing: callers read rt.Status from
-- a prior SELECT and may race with the sweeper, which can flip the row to
-- offline between that SELECT and this UPDATE. Without the predicate this
-- query would silently leave a freshly-heartbeated runtime stuck in offline.
-- Returning affected rows lets callers detect that race and fall back to
-- MarkAgentRuntimeOnline to flip the row back online.
UPDATE agent_runtime
SET last_seen_at = now()
WHERE id = $1 AND status = 'online';

-- name: TouchAgentRuntimesLastSeenBatch :execrows
-- Bulk variant of TouchAgentRuntimeLastSeen used by the BatchedHeartbeatScheduler:
-- coalesces N per-runtime "bump last_seen_at" requests into a single UPDATE so a
-- fleet beating every 15s costs ~1 DB transaction per batch tick instead of N.
--
-- Same load-bearing predicate as the single-id form: status='online' avoids
-- silently un-deleting a sweeper-flipped offline row, and we deliberately do
-- NOT touch updated_at so the rows stay HOT-eligible. Affected-rows < len(ids)
-- means some IDs raced to offline between Schedule and flush; their next beat
-- will fall through the recordHeartbeat sync path and call MarkAgentRuntimeOnline.
UPDATE agent_runtime
SET last_seen_at = now()
WHERE id = ANY(@ids::uuid[]) AND status = 'online';

-- name: MarkAgentRuntimeOnline :one
-- Used on the offline→online transition (and on first heartbeat after
-- registration). Writes status, last_seen_at, and updated_at because the
-- status flip is a real state change and we want updated_at to reflect it.
UPDATE agent_runtime
SET status = 'online', last_seen_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: SelectStaleOnlineRuntimes :many
-- Lists online runtimes whose last_seen_at exceeds the stale window. The
-- sweeper uses this as a candidate set, then optionally filters via the
-- LivenessStore before flipping rows to offline (a fresh Redis liveness
-- record means the DB row is just lagging, not actually dead).
SELECT id, workspace_id, owner_id, daemon_id, provider FROM agent_runtime
WHERE status = 'online'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision);

-- name: MarkRuntimesOfflineByIDs :many
-- Flips a known set of runtime IDs from online to offline. Paired with
-- SelectStaleOnlineRuntimes in the sweeper so the candidate selection and
-- the actual write are decoupled (the LivenessStore filter sits between).
--
-- Re-checks the stale predicate inside the UPDATE so a concurrent heartbeat
-- between the SELECT (candidate gather), the LivenessStore filter, and this
-- UPDATE cannot demote a runtime that just refreshed last_seen_at. The
-- legacy MarkStaleRuntimesOffline UPDATE had this property implicitly
-- because the predicate and the write lived in one statement; here we
-- carry it forward explicitly so the SELECT/filter/UPDATE pipeline retains
-- the same race-freedom.
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE status = 'online'
  AND id = ANY(@ids::uuid[])
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
RETURNING id, workspace_id, owner_id, daemon_id, provider;

-- name: FailTasksForOfflineRuntimes :many
-- Marks dispatched/running tasks as failed when their runtime is offline.
-- This cleans up orphaned tasks after a daemon crash or network partition.
UPDATE agent_task_queue
SET status = 'failed', completed_at = now(), error = 'runtime went offline',
    failure_reason = 'runtime_offline'
WHERE status IN ('dispatched', 'running')
  AND runtime_id IN (
    SELECT id FROM agent_runtime WHERE status = 'offline'
  )
RETURNING *;

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

-- name: PauseAgentRuntime :one
-- Marks a runtime paused. Idempotent: re-pausing an already-paused runtime
-- updates unpause_at / pause_reason but does not reset paused_at (so total
-- pause duration is preserved across re-pauses, e.g. provider returns a new
-- reset time while we're still in the original window).
UPDATE agent_runtime
SET paused_at    = COALESCE(paused_at, now()),
    unpause_at   = $2,
    pause_reason = $3,
    updated_at   = now()
WHERE id = $1
RETURNING *;

-- name: UnpauseAgentRuntime :one
-- Clears all pause fields. Idempotent on already-unpaused runtimes.
UPDATE agent_runtime
SET paused_at    = NULL,
    unpause_at   = NULL,
    pause_reason = NULL,
    updated_at   = now()
WHERE id = $1
RETURNING *;

-- name: ListRuntimesDueForUnpause :many
-- Used by the unpause sweeper to find runtimes whose scheduled unpause_at has
-- passed. Backed by idx_agent_runtime_unpause_due (partial on paused rows).
SELECT * FROM agent_runtime
WHERE paused_at IS NOT NULL
  AND unpause_at IS NOT NULL
  AND unpause_at <= now();

-- name: SuspendActiveTasksForRuntime :many
-- Called when a runtime is paused: marks any in-flight (dispatched/running)
-- task as failed with failure_reason='runtime_paused'. The matching
-- failure_reason is what the unpause path keys on to re-enqueue the work.
-- Returns affected rows so the service can broadcast task:failed events and
-- reconcile agent status.
UPDATE agent_task_queue
SET status         = 'failed',
    completed_at   = now(),
    error          = 'runtime paused',
    failure_reason = 'runtime_paused'
WHERE runtime_id = $1
  AND status IN ('dispatched', 'running')
RETURNING *;

-- name: ListResumableTasksForRuntime :many
-- Called on unpause: returns every leaf task on this runtime that the unpause
-- path should resume — both 'runtime_paused' (interrupted by the pause) and
-- retry-exhausted leaves whose original failure looked like a transient
-- provider error (rate_limit / runtime_offline / runtime_recovery / timeout).
-- "Leaf" means no descendant retry already exists, which prevents resuming a
-- chain that has already been continued via auto-retry while paused.
--
-- Bounded by the 24h window so an unpause weeks after the failure doesn't
-- silently rerun tasks the user has long since moved past. The window is
-- deliberately generous — a rate-limit pause typically resolves in hours, not
-- days, but a pause that was forgotten and manually unpaused next morning
-- should still pick up yesterday's interrupted work.
SELECT t.* FROM agent_task_queue t
WHERE t.runtime_id = $1
  AND t.status = 'failed'
  AND t.completed_at >= now() - INTERVAL '24 hours'
  AND t.failure_reason IN (
        'runtime_paused',
        'rate_limit',
        'runtime_offline',
        'runtime_recovery',
        'timeout'
      )
  AND NOT EXISTS (
        SELECT 1 FROM agent_task_queue d WHERE d.parent_task_id = t.id
      )
ORDER BY t.completed_at ASC;

-- name: DeleteStaleOfflineRuntimes :many
-- Deletes runtimes that have been offline for longer than the TTL and have
-- no agents bound (active or archived). The FK constraint on agent.runtime_id
-- is ON DELETE RESTRICT, so we must exclude all agent references.
DELETE FROM agent_runtime
WHERE status = 'offline'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
  AND id NOT IN (SELECT DISTINCT runtime_id FROM agent)
RETURNING id, workspace_id;
