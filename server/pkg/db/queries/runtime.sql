-- name: ListAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgentRuntime :one
SELECT * FROM agent_runtime
WHERE id = $1;

-- name: GetAgentRuntimes :many
-- Batch variant of GetAgentRuntime (MUL-4257): loads every runtime in the
-- input set in one round trip so the machine-level batch claim handler can
-- resolve+authorize all of a daemon's runtimes without one point query per
-- runtime. Rows are returned only for ids that exist; the caller matches them
-- back by id and skips any that are missing.
SELECT * FROM agent_runtime
WHERE id = ANY(@ids::uuid[]);

-- name: LockAgentRuntime :one
-- Acquires a row-level exclusive lock on the runtime row. Used at the
-- top of the cascade-delete transaction so that:
--   1. PostgreSQL's FK validation on agent.runtime_id (FK ... ON DELETE
--      RESTRICT) needs FOR KEY SHARE on the parent runtime row, which
--      conflicts with FOR UPDATE — so any concurrent INSERT or UPDATE
--      that would point a new/moved agent at this runtime blocks until
--      our transaction finishes; and
--   2. concurrent UPDATE/DELETE of the runtime row itself (e.g. another
--      delete attempt) waits for us to commit.
-- Combined with ListUserAgentsByRuntimeForUpdate (which row-locks active and
-- archived user agents) this closes both plan drift and archived-agent restore
-- races under read-committed isolation.
SELECT * FROM agent_runtime
WHERE id = $1
FOR UPDATE;

-- name: LockRuntimeOwnerWrites :exec
-- Workspace-scoped transaction barrier shared by every Runtime owner upsert
-- (including daemon-token COALESCE-preserve writes) and member revocation.
-- It must be taken before any Profile/Runtime relation lock.
SELECT pg_advisory_xact_lock(
    hashtextextended((sqlc.arg(workspace_id)::uuid)::text || ':runtime-owner-writes', 0)
);

-- name: LockRuntimeForCapabilityRegistration :one
-- Runtime capability/access mutations must hold this lock before discovering
-- Pool dependents. The later Agent and Task locks are acquired from the
-- post-lock ID snapshot in deterministic UUID order.
SELECT * FROM agent_runtime
WHERE id = @runtime_id
FOR UPDATE;

-- name: FindAgentRuntimeIDForBuiltinRegistration :one
SELECT id FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND provider = @provider
  AND profile_id IS NULL;

-- name: FindAgentRuntimeIDForProfileRegistration :one
SELECT id FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND profile_id = @profile_id;

-- name: ListPoolCapabilityDependentIDs :many
-- Deliberately does not lock. The caller already holds the Runtime lock, then
-- derives and locks distinct Agents before locking these exact Tasks.
SELECT id AS task_id, agent_id FROM agent_task_queue
WHERE runtime_binding_mode = 'pool'
  AND status IN ('waiting_runtime', 'queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  AND (runtime_id = @runtime_id OR session_affinity_runtime_id = @runtime_id)
ORDER BY id;

-- name: LockPoolCapabilityDependentAgents :many
SELECT * FROM agent
WHERE id = ANY(@agent_ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: LockPoolCapabilityDependents :many
SELECT * FROM agent_task_queue
WHERE id = ANY(@task_ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: RequeuePoolTaskAfterCapabilityDowngrade :one
UPDATE agent_task_queue
SET status = 'waiting_runtime', runtime_id = NULL, wait_reason = @reason
WHERE id = @task_id
  AND status = 'queued'
  AND runtime_binding_mode = 'pool'
RETURNING *;

-- name: UpdatePinnedPoolTaskWaitReason :one
UPDATE agent_task_queue
SET wait_reason = @reason
WHERE id = @task_id
  AND status IN ('waiting_runtime', 'deferred')
  AND runtime_binding_mode = 'pool'
  AND session_affinity_state = 'pinned'
RETURNING *;

-- name: UpdatePinnedPoolTaskWaitReasonCAS :one
-- A diagnosis is made from an unlocked scheduler snapshot. Every field that
-- participates in placement must still match before that diagnosis is stored;
-- otherwise an affinity/requester/requirements race would attach a stale
-- Runtime reason to a different routing snapshot.
UPDATE agent_task_queue
SET wait_reason = sqlc.arg(reason)
WHERE id = sqlc.arg(task_id)::uuid
  AND status = sqlc.arg(expected_status)
  AND runtime_id IS NOT DISTINCT FROM sqlc.narg('expected_runtime_id')::uuid
  AND agent_id = sqlc.arg(expected_agent_id)::uuid
  AND chat_session_id IS NOT DISTINCT FROM sqlc.narg('expected_chat_session_id')::uuid
  AND runtime_binding_mode = sqlc.arg(expected_runtime_binding_mode)
  AND placement_workspace_id = sqlc.arg(expected_placement_workspace_id)::uuid
  AND runtime_requester_user_id = sqlc.arg(expected_runtime_requester_user_id)::uuid
  AND runtime_trigger_user_id IS NOT DISTINCT FROM sqlc.narg('expected_runtime_trigger_user_id')::uuid
  AND runtime_requirements = sqlc.arg(expected_runtime_requirements)::jsonb
  AND session_affinity_state = sqlc.arg(expected_session_affinity_state)
  AND session_affinity_runtime_id IS NOT DISTINCT FROM sqlc.narg('expected_session_affinity_runtime_id')::uuid
  AND explicit_fresh_session = sqlc.arg(expected_explicit_fresh_session)
  AND wait_reason IS NOT DISTINCT FROM sqlc.narg('expected_wait_reason')::text
RETURNING *;

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
    capabilities,
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, @capabilities::text[], now())
-- Built-in runtimes carry no profile_id. The arbiter is the partial unique
-- index from migration 121 (WHERE profile_id IS NULL); the predicate must be
-- spelled out so Postgres selects that partial index, not the custom-runtime
-- one on (workspace_id, daemon_id, profile_id).
ON CONFLICT (workspace_id, daemon_id, provider) WHERE profile_id IS NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    capabilities = EXCLUDED.capabilities,
    last_seen_at = now(),
    updated_at = now()
RETURNING *, (xmax = 0) AS inserted;

-- name: UpsertAgentRuntimeWithProfile :one
-- Custom-runtime registration: a daemon resolved a workspace runtime_profile's
-- command_name on PATH and is registering an instance of it. The arbiter is the
-- partial unique index from migration 120 (WHERE profile_id IS NOT NULL), so a
-- single daemon can host the built-in provider AND any number of custom
-- profiles of the same protocol family. provider stays the protocol family so
-- task routing (agent.New(provider)) is unchanged; profile_id is the stable
-- identity. (xmax = 0) AS inserted mirrors UpsertAgentRuntime.
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
    profile_id,
    capabilities,
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, @capabilities::text[], now())
ON CONFLICT (workspace_id, daemon_id, profile_id) WHERE profile_id IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    provider = EXCLUDED.provider,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    capabilities = EXCLUDED.capabilities,
    last_seen_at = now(),
    updated_at = now()
RETURNING *, (xmax = 0) AS inserted;

-- name: UpdateAgentRuntimeVisibility :one
-- Toggles a runtime between 'private' (only owner can bind agents) and
-- 'public' (any workspace member can). Default for new rows is 'private'
-- (see migration 083). Gated at the handler layer to owner / workspace
-- admin only.
UPDATE agent_runtime
SET visibility = @visibility, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateAgentRuntimeCustomName :one
-- Sets or clears a runtime's user-facing custom name (MUL-4217). custom_name
-- overrides the daemon-proposed `name` for display; passing NULL reverts to
-- the default. Kept separate from the registration upserts above (which do
-- name = EXCLUDED.name on every heartbeat) so a custom name is never
-- clobbered by the daemon. Gated at the handler to owner / workspace admin.
UPDATE agent_runtime
SET custom_name = @custom_name, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateAgentRuntimeCustomNameByDaemon :many
-- Machine-level rename (MUL-4217): applies one custom name to every runtime
-- sharing a daemon_id in the workspace, since a single machine hosts one
-- runtime per provider. @owner_id is NULL for workspace owners/admins (rename
-- the whole machine) or the actor's user id otherwise (only their own
-- runtimes on that machine), so a member cannot relabel someone else's
-- runtime that happens to share the host.
UPDATE agent_runtime
SET custom_name = @custom_name, updated_at = now()
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND (@owner_id::uuid IS NULL OR owner_id = @owner_id)
RETURNING *;

-- name: ListDaemonCustomNames :many
-- Lists the custom_name of every OTHER runtime on (workspace_id, daemon_id)
-- (MUL-4217). @exclude_id drops the just-registered row. The caller derives
-- the machine-level name in Go — the same "all runtimes share one non-null
-- name" rule the frontend applies in sharedCustomName — so a freshly-added
-- runtime on an already-named machine can inherit that name and keep the
-- machine's display name stable. A daemon hosts only a handful of runtimes
-- (one per provider), so this is a tiny read.
SELECT custom_name FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND id <> @exclude_id;


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
-- Marks dispatched/running/waiting_local_directory tasks as failed when
-- their runtime is offline. This cleans up orphaned tasks after a daemon
-- crash or network partition.
UPDATE agent_task_queue
SET status = 'failed', completed_at = now(), error = 'runtime went offline',
    failure_reason = 'runtime_offline',
    wait_reason = NULL
WHERE status IN ('dispatched', 'running', 'waiting_local_directory')
  AND runtime_id IN (
    SELECT id FROM agent_runtime WHERE status = 'offline'
  )
RETURNING *;

-- name: ListAgentRuntimesByOwner :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1 AND owner_id = $2
ORDER BY created_at ASC;

-- name: ForceOfflineRuntimesByIDs :many
-- Unconditionally flips a known set of runtime IDs to offline. Distinct from
-- MarkRuntimesOfflineByIDs (which keeps a stale-window predicate so the
-- sweeper cannot demote a runtime that just heartbeated): this variant is
-- used by intentional revocation paths — e.g. removing a workspace member —
-- where the caller has already decided the runtime should be offline
-- regardless of recent liveness.
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = ANY(@runtime_ids::uuid[]) AND status = 'online'
RETURNING id, workspace_id, owner_id, daemon_id, provider;

-- name: CancelAgentTasksByRuntimeOrAgent :many
-- Cancels every active task that either lives on one of the given runtimes
-- OR belongs to one of the given agents. Used by the member-revocation flow:
-- the runtime-side covers tasks queued against the leaving member's runtimes;
-- the agent-side covers tasks pinned to a different runtime that those agents
-- left behind from a prior UpdateAgent (agent.runtime_id can change, but
-- agent_task_queue.runtime_id does not get rewritten when it does, so a task
-- queued on runtime A by agent X — later moved to runtime B — survives the
-- runtime-only revoke and could still be claimed because ClaimAgentTask does
-- not gate on agent.archived_at).
--
-- We use 'cancelled' rather than 'failed' so the daemon's per-task status
-- poller (watchTaskCancellation) interrupts the running agent gracefully.
-- Returns the affected rows so the caller can broadcast task:cancelled and
-- reconcile per-agent status.
--
-- The status list must cover EVERY non-terminal status, not just the ones the
-- daemon is actively working: 'deferred' (migration 128, comment-routing
-- escalation) was missing here and only went unnoticed because the runtime
-- delete used to cascade those rows away. Since MUL-5559 the runtime delete
-- unbinds history rows instead, and agent_task_queue_active_requires_runtime
-- rejects an active row without a runtime — so a missed status now surfaces as
-- a failed delete (runtime_delete_not_drained) instead of silent data loss.
UPDATE agent_task_queue
SET status = 'cancelled',
    completed_at = now(),
    prepare_lease_expires_at = NULL,
    runtime_id = CASE
      WHEN runtime_binding_mode = 'pool'
       AND session_affinity_state = 'pinned'
       AND session_affinity_runtime_id = ANY(@runtime_ids::uuid[])
      THEN NULL
      ELSE runtime_id
    END,
    session_affinity_state = CASE
      WHEN runtime_binding_mode = 'pool'
       AND session_affinity_state = 'pinned'
       AND session_affinity_runtime_id = ANY(@runtime_ids::uuid[])
      THEN 'removed'
      WHEN runtime_binding_mode = 'pool' AND session_affinity_state = 'unresolved'
      THEN 'none'
      ELSE session_affinity_state
    END,
    session_affinity_runtime_id = CASE
      WHEN runtime_binding_mode = 'pool'
       AND session_affinity_state IN ('pinned', 'unresolved')
       AND (
         session_affinity_state = 'unresolved'
         OR session_affinity_runtime_id = ANY(@runtime_ids::uuid[])
       )
      THEN NULL
      ELSE session_affinity_runtime_id
    END,
    wait_reason = CASE
      WHEN runtime_binding_mode = 'pool'
       AND session_affinity_state = 'pinned'
       AND session_affinity_runtime_id = ANY(@runtime_ids::uuid[])
      THEN 'session_runtime_removed'
      WHEN runtime_binding_mode = 'pool' AND session_affinity_state = 'unresolved'
      THEN NULL
      ELSE wait_reason
    END
WHERE (
    runtime_id = ANY(@runtime_ids::uuid[])
    OR session_affinity_runtime_id = ANY(@runtime_ids::uuid[])
    OR agent_id = ANY(@agent_ids::uuid[])
  )
  AND status IN ('waiting_runtime', 'queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: CountUndrainedTasksByRuntimeOrAgent :one
-- Belt-and-braces gate for the runtime-delete transaction: after cancelling,
-- every task on this runtime OR owned by an agent being unbound must be terminal
-- (completed_at IS NOT NULL) before the unbind UPDATE runs. The agent-side
-- predicate must mirror CancelAgentTasksByRuntimeOrAgent: a task can remain
-- pinned to another runtime after its agent moves. Non-zero means some
-- non-terminal status escaped the cancel query — the handler aborts with 409
-- runtime_delete_not_drained rather than letting the CHECK constraint turn it
-- into an opaque 500, and rather than deleting rows to make it go away.
SELECT count(*) FROM agent_task_queue
WHERE (
    runtime_id = ANY(@runtime_ids::uuid[])
    OR session_affinity_runtime_id = ANY(@runtime_ids::uuid[])
    OR agent_id = ANY(@agent_ids::uuid[])
  )
  AND completed_at IS NULL;

-- name: UnbindTasksFromRuntime :execrows
-- Detaches this runtime's task history so deleting the runtime row cannot
-- cascade it away (agent_task_queue.runtime_id is ON DELETE CASCADE, and
-- task_message / task_usage / task_token cascade from the task in turn).
-- Restricted to terminal rows: an active task must keep its runtime, per
-- agent_task_queue_active_requires_runtime. The caller runs
-- CancelAgentTasksByRuntimeOrAgent +
-- CountUndrainedTasksByRuntimeOrAgent first, so at this point "terminal" is
-- every row on the runtime.
UPDATE agent_task_queue
SET runtime_id = NULL
WHERE runtime_id = $1 AND completed_at IS NOT NULL;

-- name: UnbindUserAgentsFromRuntime :many
-- MUL-5559: the runtime-delete replacement for archive-then-hard-delete. Every
-- user agent bound to this runtime becomes unbound (runtime_id IS NULL) and
-- keeps its row, chats, labels, channel installations and autopilot config.
--
-- Deliberately NOT filtered on archived_at: an agent archived earlier is just
-- as much the user's data as an active one, and hard-deleting it was the same
-- bug. Deliberately restricted to kind = 'user': system agents are invisible
-- execution infrastructure with no UI to rebind them (see
-- DeleteSystemAgentsByRuntime), so leaving them unbound would strand rows no
-- one can repair.
UPDATE agent
SET runtime_id = NULL, updated_at = now()
WHERE runtime_id = $1 AND kind = 'user'
RETURNING *;

-- name: DeleteAgentRuntime :exec
DELETE FROM agent_runtime WHERE id = $1;

-- name: DeleteSystemAgentsByRuntime :exec
-- System agents are invisible execution infrastructure (for example the Agent
-- Builder). Remove them before deleting their runtime so the RESTRICT runtime
-- FK cannot block an otherwise dependency-free delete.
DELETE FROM agent WHERE runtime_id = $1 AND kind = 'system';

-- name: CountActiveAgentsByRuntime :one
SELECT count(*) FROM agent WHERE runtime_id = $1 AND archived_at IS NULL;

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

-- name: ListStaleOfflineRuntimesForGC :many
-- Bounded, unlocked discovery only. Each candidate is revalidated under its
-- Runtime row lock before the shared Runtime -> ChatSession -> Agent -> Task
-- teardown runs; direct DELETE would bypass Pool affinity/history cleanup.
SELECT id, workspace_id
FROM agent_runtime
WHERE status = 'offline'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
  AND NOT EXISTS (
    SELECT 1
    FROM agent
    WHERE agent.runtime_id = agent_runtime.id
  )
ORDER BY last_seen_at, id
LIMIT @candidate_limit::integer;

-- name: LockStaleOfflineRuntimeForGC :one
-- Recheck mutable Runtime eligibility after taking the first relation lock in
-- the common deletion order. Bound-Agent absence is deliberately re-read by a
-- separate statement after this lock so a just-committed FK reference cannot
-- be hidden by the SELECT snapshot.
SELECT *
FROM agent_runtime
WHERE id = @runtime_id
  AND status = 'offline'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
FOR UPDATE;

-- name: RuntimeHasBoundAgents :one
SELECT EXISTS (
  SELECT 1 FROM agent WHERE runtime_id = @runtime_id
);

-- name: ListPoolRuntimeCandidates :many
-- Fresh placement only. Authorization, capability, strict DB idle and the
-- complete deterministic rank all precede the bound. Liveness is filtered in
-- Go, in this order, before any placement transaction begins.
SELECT sqlc.embed(ar), count(fixed_agent.id)::bigint AS fixed_binding_count
FROM agent_runtime AS ar
JOIN member AS m
  ON m.workspace_id = ar.workspace_id
 AND m.user_id = sqlc.arg(requester_user_id)::uuid
LEFT JOIN agent AS fixed_agent
  ON fixed_agent.runtime_id = ar.id
 AND fixed_agent.runtime_binding_mode = 'fixed'
 AND fixed_agent.archived_at IS NULL
WHERE ar.workspace_id = sqlc.arg(workspace_id)::uuid
  AND ar.status = 'online'
  AND ar.capabilities @> sqlc.arg(requirements_all)::text[]
  AND (
    ar.runtime_mode = 'cloud'
    OR (
      ar.runtime_mode = 'local'
      AND ar.owner_id = sqlc.narg(trigger_user_id)::uuid
    )
  )
  AND NOT EXISTS (
    SELECT 1 FROM agent_task_queue AS occupied
    WHERE occupied.runtime_id = ar.id
      AND occupied.status IN (
        'queued', 'deferred', 'dispatched', 'running',
        'waiting_local_directory'
      )
  )
GROUP BY ar.id
ORDER BY
  CASE
    WHEN ar.runtime_mode = 'local'
     AND ar.owner_id = sqlc.narg(trigger_user_id)::uuid THEN 0
    ELSE 1
  END,
  ar.last_seen_at DESC NULLS LAST,
  fixed_binding_count ASC,
  ar.created_at ASC,
  ar.id ASC
LIMIT sqlc.arg(runtime_limit);

-- name: GetPinnedPoolRuntimeCandidate :one
-- Session affinity deliberately ignores occupancy: once eligible and alive,
-- the Task joins the Runtime's existing queue rather than switching machines.
SELECT ar.* FROM agent_runtime AS ar
JOIN member AS m
  ON m.workspace_id = ar.workspace_id
 AND m.user_id = sqlc.arg(requester_user_id)::uuid
WHERE ar.id = sqlc.arg(runtime_id)::uuid
  AND ar.workspace_id = sqlc.arg(workspace_id)::uuid
  AND ar.status = 'online'
  AND ar.capabilities @> sqlc.arg(requirements_all)::text[]
  AND (
    ar.runtime_mode = 'cloud'
    OR (
      ar.runtime_mode = 'local'
      AND ar.owner_id = sqlc.narg(trigger_user_id)::uuid
    )
  );

-- name: LockPoolRuntimeForPlacement :one
SELECT * FROM agent_runtime
WHERE id = sqlc.arg(runtime_id)::uuid
FOR UPDATE SKIP LOCKED;

-- name: CountRuntimeCapacityBearingTasks :one
SELECT count(*) FROM agent_task_queue
WHERE runtime_id = sqlc.arg(runtime_id)::uuid
  AND status IN (
    'queued', 'deferred', 'dispatched', 'running',
    'waiting_local_directory'
  );
