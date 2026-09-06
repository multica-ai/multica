-- name: ListAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListAgentRuntimeIDsByWorkspace :many
SELECT id FROM agent_runtime
WHERE workspace_id = $1
ORDER BY id ASC;

-- name: ListVisibleAgentRuntimes :many
-- A private runtime is another member's machine and must not leak into their
-- runtime list. The owner can always see their own runtime; everyone else
-- sees only runtimes the owner has explicitly shared with the workspace.
SELECT * FROM agent_runtime
WHERE workspace_id = $1
  AND (owner_id = $2 OR visibility = 'public')
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

-- name: GetAgentRuntimeHeartbeatLeases :many
-- Narrow connection-time and heartbeat-reconciliation projection. The daemon
-- WebSocket authenticates its whole runtime set in one round trip and then
-- keeps these immutable ownership fields plus liveness state in its connection
-- lease, avoiding a GetAgentRuntime call on every heartbeat.
SELECT id, workspace_id, daemon_id, status, last_seen_at
FROM agent_runtime
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
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (workspace_id, daemon_id, profile_id) WHERE profile_id IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    provider = EXCLUDED.provider,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
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

-- name: TouchAgentRuntimesLastSeenBatch :many
-- Bulk variant of TouchAgentRuntimeLastSeen used by the BatchedHeartbeatScheduler:
-- coalesces N per-runtime "bump last_seen_at" requests into a single UPDATE so a
-- fleet beating every 15s costs ~1 DB transaction per batch tick instead of N.
--
-- Same load-bearing predicate as the single-id form: status='online' avoids
-- silently un-deleting a sweeper-flipped offline row, and we deliberately do
-- NOT touch updated_at so the rows stay HOT-eligible. RETURNING is load-bearing:
-- the scheduler reconciles omitted IDs in one narrow batch query, restoring
-- sweeper-raced offline rows and invalidating connections for deleted rows.
UPDATE agent_runtime
SET last_seen_at = now()
WHERE id = ANY(@ids::uuid[]) AND status = 'online'
RETURNING id;

-- name: MarkAgentRuntimeOnline :one
-- Used on the offline→online transition (and on first heartbeat after
-- registration). Writes status, last_seen_at, and updated_at because the
-- status flip is a real state change and we want updated_at to reflect it.
UPDATE agent_runtime
SET status = 'online', last_seen_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkAgentRuntimeOnlineIfOffline :execrows
-- Reports whether this heartbeat performed an offline -> online transition.
-- The conditional update prevents concurrent stale heartbeat snapshots from
-- publishing duplicate lifecycle refresh events after another beat already
-- recovered the runtime.
UPDATE agent_runtime
SET status = 'online', last_seen_at = now(), updated_at = now()
WHERE id = $1 AND status <> 'online';

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: SetAgentRuntimeOfflineWithReason :exec
-- Takes a runtime offline and records WHY, for the one class of cause the user
-- has to repair before the runtime can come back (MUL-6164). Everything that
-- merely stops — daemon shutdown, laptop asleep — uses SetAgentRuntimeOffline
-- and leaves no reason, because "wait for it" needs no explanation.
--
-- Merged into metadata rather than a column: registration overwrites metadata
-- wholesale (`metadata = EXCLUDED.metadata`), so a runtime that comes back
-- drops the reason as a side effect of being usable again — there is no stale
-- explanation to clean up and no code path that has to remember to clear it.
UPDATE agent_runtime
SET status = 'offline',
    metadata = metadata || jsonb_build_object('offline_reason', @offline_reason::jsonb),
    updated_at = now()
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
-- their runtime has remained offline beyond the reconnect grace. A short or
-- medium network partition must not terminate a daemon process that is still
-- running locally; a real daemon restart is recovered separately through
-- RecoverOrphanedTasksForRuntime. Bounded per tick so a large recovery backlog
-- cannot monopolise the sweeper transaction. last_seen_at is normally set by
-- the first heartbeat; updated_at is only the fallback for a never-heartbeated
-- runtime, and a forced-offline write starts its grace from that update.
WITH victims AS (
  SELECT task.id
  FROM agent_task_queue task
  JOIN agent_runtime runtime ON runtime.id = task.runtime_id
  WHERE task.status IN ('dispatched', 'running', 'waiting_local_directory')
    AND runtime.status = 'offline'
    AND COALESCE(runtime.last_seen_at, runtime.updated_at) <
        now() - make_interval(secs => @reconnect_grace_secs::double precision)
  ORDER BY COALESCE(runtime.last_seen_at, runtime.updated_at), task.created_at
  LIMIT @max_per_tick::int
  FOR UPDATE OF task SKIP LOCKED
)
UPDATE agent_task_queue AS task
SET status = 'failed', completed_at = now(), error = 'runtime went offline',
    failure_reason = 'runtime_offline',
    wait_reason = NULL
FROM victims
WHERE task.id = victims.id
  AND task.status IN ('dispatched', 'running', 'waiting_local_directory')
RETURNING task.*;

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
SET status = 'cancelled', completed_at = now()
WHERE (runtime_id = ANY(@runtime_ids::uuid[]) OR agent_id = ANY(@agent_ids::uuid[]))
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
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
WHERE (runtime_id = ANY(@runtime_ids::uuid[]) OR agent_id = ANY(@agent_ids::uuid[]))
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

-- name: LockWorkspaceForRuntimeMerge :exec
-- Step 1 of the legacy runtime merge's fence, and the same first step every task
-- write takes (lock_task_owner_rows, migration 284): the workspace row, FOR KEY
-- SHARE. Taking it here rather than relying on the fence inside the reassignment
-- keeps the merge's lock order identical to the writers' — workspaces before owner
-- rows — so the two can never hold each other's next lock (MUL-5999).
SELECT 1 FROM workspace w
WHERE w.id IN (
    SELECT r.workspace_id FROM agent_runtime r WHERE r.id = ANY(@runtime_ids::uuid[])
)
ORDER BY w.id
FOR KEY SHARE;

-- name: LockRuntimesForMerge :many
-- Step 2: the runtime rows themselves, FOR UPDATE, in id order.
--
-- FOR UPDATE is the point. A task write's fence takes FOR KEY SHARE on the runtime
-- it references, and KEY SHARE conflicts with UPDATE but not with another KEY
-- SHARE — so while the merge held only the workspace's KEY SHARE, a concurrent
-- enqueue against the OLD runtime went straight through after the task scan and was
-- then silently removed by ON DELETE CASCADE when the old runtime was deleted.
-- Holding FOR UPDATE on both runtimes from before the scan until COMMIT means a
-- late writer either commits first (and the scan sees its task) or waits and finds
-- the runtime gone, which its fence reports as "no row written".
--
-- Both runtimes are locked in one ordered statement so two merges running in
-- opposite directions cannot take the same pair in opposite orders.
--
-- Returns the ids it actually locked: a caller that asked for two and got fewer
-- knows a runtime disappeared before it got there and must abandon the merge.
SELECT id FROM agent_runtime
WHERE id = ANY(@runtime_ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: ReassignTasksToRuntime :one
-- Fenced against workspace teardown: lock_task_owner_rows (migration 284)
-- locks the owners' workspace rows in the writer's own transaction and returns
-- false once they are gone, so this statement writes no row instead of stranding
-- a task in a workspace that has just been deleted (MUL-5999).
-- Re-points every queued/running/completed task referencing old_runtime_id.
-- Required before deleting the old runtime row because agent_task_queue has
-- an ON DELETE CASCADE FK that would otherwise drop historical tasks.
--
-- Returns the fence verdict separately from the row count on purpose. "0 rows"
-- is ambiguous — it means either "the old runtime had no tasks" or "the fence
-- refused" — and a caller that cannot tell them apart would go on to delete the
-- old runtime, letting that same ON DELETE CASCADE drop the very history this
-- statement exists to preserve. FenceOk = false must abort the merge.
WITH fence AS MATERIALIZED (
    -- Once per statement rather than once per row: the predicate is VOLATILE, so
    -- calling it from the WHERE clause of a bulk UPDATE would re-run it for every
    -- candidate row.
    SELECT lock_task_owner_rows(NULL, NULL, @new_runtime_id) AS ok
),
reassigned AS (
    UPDATE agent_task_queue
    SET runtime_id = @new_runtime_id
    WHERE runtime_id = @old_runtime_id
      AND (SELECT ok FROM fence)
    RETURNING id
)
SELECT
    (SELECT ok FROM fence) AS fence_ok,
    (SELECT count(*) FROM reassigned) AS reassigned_tasks;

-- name: RecordRuntimeLegacyDaemonID :exec
-- Remembers the most recent hostname-derived daemon_id that was merged into
-- this row. Useful for debugging when tracing back why a given runtime row
-- subsumed an old one, and only overwrites NULL so the earliest merge is
-- preserved.
UPDATE agent_runtime
SET legacy_daemon_id = COALESCE(legacy_daemon_id, $2)
WHERE id = $1;

-- name: ListStaleOfflineRuntimeGCCandidates :many
-- Bounded gather for runtime GC. Non-terminal task owners are deliberately
-- excluded here so one permanently-deferred task cannot monopolise the front
-- of every batch and starve otherwise-drainable runtimes. The per-runtime
-- transaction re-checks every predicate after taking FOR UPDATE, so this is an
-- efficiency filter rather than the correctness boundary.
SELECT id FROM agent_runtime
WHERE status = 'offline'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
  AND NOT EXISTS (
    SELECT 1
    FROM agent
    WHERE agent.runtime_id = agent_runtime.id
      AND agent.kind = 'user'
      AND agent.archived_at IS NULL
  )
  AND NOT EXISTS (
    SELECT 1
    FROM agent_task_queue
    WHERE agent_task_queue.runtime_id = agent_runtime.id
      AND agent_task_queue.completed_at IS NULL
  )
ORDER BY last_seen_at ASC, id ASC
LIMIT @max_per_tick::int;

-- name: IsAgentRuntimeEligibleForGC :one
-- Re-checks the mutable GC predicates after the caller has locked the runtime
-- row FOR UPDATE. Agent inserts/updates and task ownership writes take FOR KEY
-- SHARE on that row, so no new dependency can commit between this check and
-- DeleteAgentRuntime in the same transaction.
SELECT EXISTS (
  SELECT 1 FROM agent_runtime
  WHERE agent_runtime.id = @id
    AND status = 'offline'
    AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
    AND NOT EXISTS (
      SELECT 1
      FROM agent
      WHERE agent.runtime_id = agent_runtime.id
        AND agent.kind = 'user'
        AND agent.archived_at IS NULL
    )
) AS eligible;

-- name: CountTasksByRuntime :one
-- Final fail-closed assertion after UnbindTasksFromRuntime. A non-zero result
-- aborts the transaction instead of relying on the legacy ON DELETE CASCADE.
SELECT count(*) FROM agent_task_queue WHERE runtime_id = $1;
