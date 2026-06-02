-- name: ListAgents :many
SELECT * FROM agent
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at ASC;

-- name: ListAllAgents :many
SELECT * FROM agent
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgent :one
SELECT * FROM agent
WHERE id = $1;

-- name: GetAgentInWorkspace :one
SELECT * FROM agent
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAgent :one
INSERT INTO agent (
    workspace_id, name, description, avatar_url, runtime_mode,
    runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id,
    instructions, custom_env, custom_args, mcp_config, model, thinking_level
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
RETURNING *;

-- name: UpdateAgent :one
UPDATE agent SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    runtime_config = COALESCE(sqlc.narg('runtime_config'), runtime_config),
    runtime_mode = COALESCE(sqlc.narg('runtime_mode'), runtime_mode),
    runtime_id = COALESCE(sqlc.narg('runtime_id'), runtime_id),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    status = COALESCE(sqlc.narg('status'), status),
    max_concurrent_tasks = COALESCE(sqlc.narg('max_concurrent_tasks'), max_concurrent_tasks),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    custom_env = COALESCE(sqlc.narg('custom_env'), custom_env),
    custom_args = COALESCE(sqlc.narg('custom_args'), custom_args),
    mcp_config = COALESCE(sqlc.narg('mcp_config'), mcp_config),
    model = COALESCE(sqlc.narg('model'), model),
    thinking_level = COALESCE(sqlc.narg('thinking_level'), thinking_level),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentThinkingLevel :one
-- Explicit NULL-clear for thinking_level. COALESCE-based UpdateAgent cannot
-- set the column back to NULL, so the API layer routes "user picked Default"
-- through this dedicated query.
UPDATE agent SET thinking_level = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentMcpConfig :one
UPDATE agent SET mcp_config = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateAgentCustomEnv :one
-- Replaces an agent's custom_env map wholesale. Used by the dedicated
-- env-management endpoint (POST/PUT /api/agents/{id}/env), which is the
-- only post-creation write path for env. UpdateAgent has been stripped
-- of custom_env handling so all env mutations flow through here and the
-- handler's audit-log + **** sentinel guard.
UPDATE agent
SET custom_env = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAgent :one
UPDATE agent SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAgentsByRuntime :many
-- Bulk-archives every active agent bound to any runtime in the given set.
-- Used when revoking a leaving member's runtimes so agents pinned to those
-- runtimes can no longer be assigned new work. Returns the affected rows so
-- the caller can broadcast agent:archived per agent.
UPDATE agent
SET archived_at = now(), archived_by = @archived_by, updated_at = now()
WHERE runtime_id = ANY(@runtime_ids::uuid[]) AND archived_at IS NULL
RETURNING *;

-- name: ArchiveAgentsByIDs :many
-- Narrow archive that only touches the explicit ID list. Used by the
-- cascade-delete endpoint so the user's expected_active_agent_ids list
-- is the authoritative bound on what gets archived: any agent that
-- appeared on the runtime after the user opened the dialog is filtered
-- out here so it can't be silently archived even in the (vanishingly
-- rare) case where a row-level race slips past the runtime FOR UPDATE
-- lock. Returns the affected rows so the caller can broadcast
-- agent:archived per agent.
UPDATE agent
SET archived_at = now(), archived_by = @archived_by, updated_at = now()
WHERE id = ANY(@agent_ids::uuid[]) AND archived_at IS NULL
RETURNING *;

-- name: ListActiveAgentsByRuntime :many
-- Returns every non-archived agent bound to a runtime. Backs the cascade
-- delete dialog: when DELETE /api/runtimes/:id refuses with
-- runtime_has_active_agents, the response carries this list so the front-end
-- can render exactly the agents that will be archived if the user confirms,
-- and so the cascade endpoint's expected_active_agent_ids check has a stable
-- snapshot to compare against. Ordered by name for a deterministic display.
SELECT * FROM agent
WHERE runtime_id = $1 AND archived_at IS NULL
ORDER BY name ASC;

-- name: ListActiveAgentsByRuntimeForUpdate :many
-- FOR UPDATE variant used inside the cascade-delete transaction. Locks
-- each currently-active agent row so a concurrent archive/move of one
-- of those rows blocks until our transaction commits. Pair with
-- LockAgentRuntime, which holds the runtime row exclusively to also
-- block FK-validated INSERTs / runtime_id updates that would otherwise
-- add a new agent to the runtime mid-cascade. Together they guarantee
-- that the set we compared against expected_active_agent_ids is exactly
-- the set ArchiveAgentsByIDs will operate on — no race window.
SELECT * FROM agent
WHERE runtime_id = $1 AND archived_at IS NULL
ORDER BY name ASC
FOR UPDATE;

-- name: RestoreAgent :one
UPDATE agent SET archived_at = NULL, archived_by = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAgentTasks :many
SELECT * FROM agent_task_queue
WHERE agent_id = $1
ORDER BY created_at DESC;

-- name: CreateAgentTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, trigger_comment_id,
    trigger_summary, force_fresh_session, is_leader_task
)
VALUES (
    $1, $2, $3, 'queued', $4, sqlc.narg(trigger_comment_id),
    sqlc.narg(trigger_summary),
    COALESCE(sqlc.narg('force_fresh_session')::boolean, FALSE),
    COALESCE(sqlc.narg('is_leader_task')::boolean, FALSE)
)
RETURNING *;

-- name: CreateQuickCreateTask :one
-- Quick-create tasks have no issue / chat / autopilot link; the entire job
-- description (prompt, requester, workspace) lives in context JSONB. The
-- daemon detects this variant via context.type == "quick_create".
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, context)
VALUES ($1, $2, NULL, 'queued', $3, $4)
RETURNING *;

-- name: LinkTaskToIssue :exec
-- Attaches the issue a quick-create task produced back to the task row, once
-- the agent has finished and the issue exists. Guarded by `issue_id IS NULL`
-- so this never overwrites an issue id that was set at task creation (only
-- quick-create tasks land here unset). Fixes the activity row staying on
-- "Creating issue" forever after completion.
UPDATE agent_task_queue
SET issue_id = $2
WHERE id = $1 AND issue_id IS NULL;

-- name: CreateRetryTask :one
-- Clones a parent task into a fresh queued attempt. Carries forward the
-- agent's resume context (session_id/work_dir) so the child can continue
-- the conversation when the backend supports it. Resume-unsafe failures are
-- retried as fresh sessions so the child does not inherit a stuck agent
-- conversation. Keep the CASE WHEN predicates in sync with
-- resumeUnsafeFailureReason and the resume lookup blacklists. attempt is
-- incremented; max_attempts, trigger_comment_id, and is_leader_task are
-- inherited so the retried task keeps the same squad-role provenance as its
-- parent and the self-trigger guard in shouldEnqueueSquadLeaderOnComment
-- continues to recognise it as a leader task.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, chat_session_id, autopilot_run_id,
    status, priority, trigger_comment_id, trigger_summary, context,
    session_id, work_dir,
    attempt, max_attempts, parent_task_id, force_fresh_session, is_leader_task
)
SELECT
    p.agent_id, p.runtime_id, p.issue_id, p.chat_session_id, p.autopilot_run_id,
    'queued', p.priority, p.trigger_comment_id, p.trigger_summary, p.context,
    CASE WHEN p.failure_reason IS NOT DISTINCT FROM 'codex_semantic_inactivity' THEN NULL ELSE p.session_id END,
    CASE WHEN p.failure_reason IS NOT DISTINCT FROM 'codex_semantic_inactivity' THEN NULL ELSE p.work_dir END,
    p.attempt + 1, p.max_attempts, p.id,
    p.failure_reason IS NOT DISTINCT FROM 'codex_semantic_inactivity',
    p.is_leader_task
FROM agent_task_queue p
WHERE p.id = $1
RETURNING *;

-- name: CancelAgentTasksByIssue :many
-- Cancels every active task on the issue and returns the affected rows so the
-- caller can reconcile each agent's status and broadcast task:cancelled events
-- (#1587). Prior :exec form silently dropped that info, so internal cancel
-- paths (issue status flips to cancelled/done, etc.) left agents stuck at
-- status="working" with no self-correction.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: CancelAgentTasksByIssueAndAgent :many
-- Cancels active tasks for a single (issue, agent) pair without touching
-- tasks belonging to other agents on the same issue. Used by the manual
-- rerun flow so re-running the assignee doesn't collateral-cancel a
-- still-running @-mention agent on the same issue.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: CancelAgentTasksByAgent :many
-- Bulk-cancel every active (queued/dispatched/running) task for an agent.
-- Returns the affected rows so callers can broadcast task:cancelled events.
-- Mirrors the shape of CancelAgentTasksByIssue / CancelAgentTasksByIssueAndAgent
-- (also :many + RETURNING + completed_at) so the three sibling cancel paths
-- behave consistently.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE agent_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: CancelAgentTasksByTriggerComment :many
-- Cancels active tasks whose trigger is the given comment. Called when a
-- comment is deleted so the agent does not run with the now-deleted content
-- already embedded in its prompt. Must run BEFORE the comment row is deleted
-- because the FK ON DELETE SET NULL would otherwise nullify trigger_comment_id
-- and we'd lose the ability to find the affected tasks.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE trigger_comment_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: CancelAgentTasksByChatSession :many
-- Cancels active tasks belonging to a chat session. Called from
-- DeleteChatSession so the daemon doesn't keep running work whose result
-- has nowhere to land. Must run BEFORE the chat_session row is deleted —
-- the FK ON DELETE SET NULL would otherwise nullify chat_session_id and we
-- could no longer reach those tasks.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE chat_session_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: GetAgentTask :one
SELECT * FROM agent_task_queue
WHERE id = $1;

-- name: GetAgentTaskInWorkspace :one
-- Loads a task only when its owning agent lives in the given workspace.
-- agent_id is NOT NULL on every task row (and ON DELETE CASCADE, so the agent
-- always exists), which makes this the universal tenant guard for
-- user-initiated cancellation — independent of which optional source FK
-- (issue / chat_session / autopilot_run) happens to be set. It is what lets
-- run_only autopilot tasks and quick_create tasks (whose issue does not exist
-- yet) be cancelled at all, instead of 404-ing on a missing source FK.
SELECT atq.* FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE atq.id = $1 AND a.workspace_id = $2;

-- name: ClaimAgentTask :one
-- Claims the next queued task for an agent, enforcing per-(issue, agent) serialization:
-- a task is only claimable when no other task for the same issue AND same agent is
-- already dispatched or running. This allows different agents to work on the same
-- issue in parallel while preventing a single agent from running duplicate tasks.
-- Chat tasks (issue_id IS NULL) use chat_session_id for serialization instead.
-- Quick-create tasks have no issue / chat / autopilot link, so they serialize on
-- "any other quick-create-shaped task" (all four FKs NULL) for the same agent —
-- otherwise a user mashing the create button could fire concurrent quick-creates
-- whose completion lookup would race over "most recent issue by this agent".
UPDATE agent_task_queue
SET status = 'dispatched', dispatched_at = now()
WHERE id = (
    SELECT atq.id FROM agent_task_queue atq
    WHERE atq.agent_id = $1 AND atq.status = 'queued'
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue active
          WHERE active.agent_id = atq.agent_id
            AND active.status IN ('dispatched', 'running', 'waiting_local_directory')
            AND (
              (atq.issue_id IS NOT NULL AND active.issue_id = atq.issue_id)
              OR (atq.chat_session_id IS NOT NULL AND active.chat_session_id = atq.chat_session_id)
              OR (
                atq.issue_id IS NULL
                AND atq.chat_session_id IS NULL
                AND atq.autopilot_run_id IS NULL
                AND active.issue_id IS NULL
                AND active.chat_session_id IS NULL
                AND active.autopilot_run_id IS NULL
              )
            )
      )
    ORDER BY atq.priority DESC, atq.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ReclaimStaleDispatchedTaskForRuntime :one
-- Re-delivers a task whose previous claim likely succeeded server-side but
-- whose response never reached the daemon. The task is still in `dispatched`
-- with no `started_at`, so the daemon has not acknowledged it via StartTask.
-- Refresh dispatched_at so the server-side dispatch timeout measures from the
-- recovered delivery attempt.
UPDATE agent_task_queue
SET dispatched_at = now()
WHERE id = (
    SELECT atq.id FROM agent_task_queue atq
    WHERE atq.runtime_id = $1
      AND atq.status = 'dispatched'
      AND atq.started_at IS NULL
      AND atq.dispatched_at < now() - make_interval(secs => @claim_recovery_secs::double precision)
    ORDER BY atq.priority DESC, atq.dispatched_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: StartAgentTask :one
-- Transitions a task to running. Accepts either 'dispatched' (the normal
-- claim → run flow) or 'waiting_local_directory' (the daemon held the row in
-- a wait state while another task owned the local_directory path lock; once
-- the lock was acquired the daemon flips here). wait_reason is cleared on
-- the transition so a future read can't conflate "currently waiting" with
-- "previously waited".
UPDATE agent_task_queue
SET status = 'running', started_at = now(), wait_reason = NULL
WHERE id = $1 AND status IN ('dispatched', 'waiting_local_directory')
RETURNING *;

-- name: MarkAgentTaskWaitingLocalDirectory :one
-- Transitions a freshly-dispatched task into 'waiting_local_directory' while
-- the daemon waits for another in-flight task to release the path lock on a
-- project_resource of type local_directory. wait_reason carries a short
-- human-readable hint (typically the contested path) that the UI surfaces
-- alongside the status.
--
-- The CHECK only allows the transition from 'dispatched' so a daemon can't
-- mark an already-running or terminal task as waiting; the StartAgentTask
-- mutation handles the reverse transition once the lock is acquired.
UPDATE agent_task_queue
SET status = 'waiting_local_directory', wait_reason = $2
WHERE id = $1 AND status = 'dispatched'
RETURNING *;

-- name: CompleteAgentTask :one
UPDATE agent_task_queue
SET status = 'completed', completed_at = now(), result = $2, session_id = $3, work_dir = $4
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: GetLastTaskSession :one
-- Returns the session_id and work_dir from the most recent task for a given
-- (agent_id, issue_id) pair, used for session resumption on the auto-retry
-- path. We accept both 'completed' and 'failed' tasks: a failed task may
-- have established a real agent session before crashing (orphaned by a
-- daemon restart, runtime offline, or sweeper timeout), and the daemon pins
-- the resume pointer mid-flight via UpdateAgentTaskSession. Without this,
-- an auto-retry of a mid-run failure would silently start a fresh
-- conversation and lose the in-flight context — exactly what MUL-1128's B
-- branch is meant to fix.
--
-- Manual rerun (TaskService.RerunIssue) does NOT take this path: it sets
-- force_fresh_session=true on the new task, and the daemon claim handler
-- skips this lookup entirely. The user already judged the prior output bad;
-- resuming the same conversation would replay a poisoned state.
--
-- Tasks that ended in a known "poisoned" terminal state are also excluded
-- here so even auto-retry does not inherit the bad session. The daemon
-- classifies these failures (iteration_limit, agent_fallback_message,
-- api_invalid_request, codex_semantic_inactivity) when it detects either an
-- agent fallback marker in the output, an upstream API 400 that means the
-- conversation history itself is unprocessable (oversized image, malformed
-- base64, etc.), or a Codex semantic inactivity timeout whose recorded
-- session may replay the same stuck state.
--
-- The error-text ILIKE clause is defense-in-depth for the api_invalid_request
-- shape: a legacy row tagged 'agent_error' (pre-MUL-1921), a deploy-window
-- row that the old code wrote between migration and rollout, or a future
-- error format that escapes the daemon classifier all still get filtered
-- here as long as the canonical Anthropic 400 marker is present in the
-- error text. Migration 079 backfills the failure_reason column itself,
-- so observability stays accurate; this clause guarantees session resume
-- never picks up a bad session even when failure_reason hasn't caught up.
SELECT session_id, work_dir, runtime_id FROM agent_task_queue
WHERE agent_id = $1 AND issue_id = $2
  AND (
    status = 'completed'
    OR (
      status = 'failed'
      AND COALESCE(failure_reason, '') NOT IN ('iteration_limit', 'agent_fallback_message', 'api_invalid_request', 'codex_semantic_inactivity')
      AND NOT (COALESCE(error, '') ILIKE '%400%' AND COALESCE(error, '') ILIKE '%invalid_request_error%')
    )
  )
  AND session_id IS NOT NULL
ORDER BY COALESCE(completed_at, started_at, dispatched_at, created_at) DESC
LIMIT 1;

-- name: GetLastTaskStartedAtForIssueAndAgent :one
-- Returns the started_at of the most recent prior task for this (agent, issue)
-- pair, used as the "since" anchor for counting comments that arrived since the
-- agent's last run. Any terminal state counts as "a run happened". Tasks with
-- no started_at (never dispatched / the just-claimed current task) are excluded,
-- so this never returns the current claim's own row. MUST use started_at, never
-- completed_at: a long run would otherwise miss comments posted while it ran.
SELECT started_at FROM agent_task_queue
WHERE agent_id = $1 AND issue_id = $2 AND started_at IS NOT NULL
ORDER BY started_at DESC
LIMIT 1;

-- name: FailAgentTask :one
-- Marks a task as failed. session_id and work_dir are merged via COALESCE so
-- if the agent already established a real session before failing (e.g. it
-- crashed mid-conversation, was cancelled, or hit a tool error) the resume
-- pointer is preserved on the task row. The next chat task can then fall
-- back to GetLastChatTaskSession and continue the conversation instead of
-- silently starting over.
--
-- failure_reason is a coarse classifier consumed by the auto-retry path;
-- 'agent_error' is the safe default when the daemon doesn't supply one.
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = $2,
    failure_reason = COALESCE(sqlc.narg('failure_reason'), 'agent_error'),
    session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir = COALESCE(sqlc.narg('work_dir'), work_dir)
WHERE id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: UpdateAgentTaskSession :exec
-- Pins the resume pointer mid-flight so a daemon crash leaves a usable
-- session_id/work_dir on the task row. No-op if the task is no longer
-- in dispatched/running. waiting_local_directory tasks have no session yet
-- so this query intentionally skips them.
UPDATE agent_task_queue
SET session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir  = COALESCE(sqlc.narg('work_dir'), work_dir)
WHERE id = $1 AND status IN ('dispatched', 'running');

-- name: RecoverOrphanedTasksForRuntime :many
-- Called by the daemon at startup. Atomically fails any dispatched/running/
-- waiting_local_directory task that the prior incarnation of this runtime
-- owned but did not finalize. Returns the failed rows so callers can hand
-- them to the auto-retry path. waiting_local_directory rows are included
-- because the daemon holding the path lock is the same process that just
-- died — without us, the row would sit waiting forever.
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = 'daemon restarted while task was in flight',
    failure_reason = 'runtime_recovery',
    wait_reason = NULL
WHERE runtime_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: FailStaleTasks :many
-- Fails tasks stuck in dispatched/running beyond the given thresholds.
-- Handles cases where the daemon is alive but the task is orphaned
-- (e.g. agent process hung, daemon failed to report completion).
-- waiting_local_directory rows are intentionally excluded: the daemon owns
-- the wait (with its own ctx-driven timeout) and a legitimate queue ahead
-- of this task can exceed the dispatch / running timeouts without being
-- "stuck". If the daemon dies, RecoverOrphanedTasksForRuntime reclaims
-- those rows at restart.
UPDATE agent_task_queue
SET status = 'failed', completed_at = now(), error = 'task timed out',
    failure_reason = 'timeout'
WHERE (status = 'dispatched' AND dispatched_at < now() - make_interval(secs => @dispatch_timeout_secs::double precision))
   OR (status = 'running' AND started_at < now() - make_interval(secs => @running_timeout_secs::double precision))
RETURNING *;

-- name: ExpireStaleQueuedTasks :many
-- Fails tasks that have been sitting in 'queued' for longer than the TTL.
-- This is the cleanup arm of the MUL-1899 "queued backlog" fix: even with the
-- new dispatch-time admission gate that refuses to enqueue when the runtime
-- is offline, we still need to drain the historical 87k+ doomed rows and
-- handle edge cases where a runtime goes offline AFTER a task is already
-- queued (the admission check protects new enqueues, not in-flight queue
-- depth).
--
-- Concurrency safety: the daemon's claim path may race with this sweeper to
-- transition the same row out of 'queued'. We protect against that two
-- ways:
--   1. The CTE selects victims with FOR UPDATE SKIP LOCKED so a row that is
--      currently being claimed (or otherwise locked) is skipped — no lock
--      contention with the dispatch path, and we won't queue up behind it.
--   2. The outer UPDATE re-checks status='queued' AND the TTL predicate at
--      apply time. If a daemon claimed the row between selection and update
--      (e.g. lock released after the claim transaction commits), the row is
--      already 'dispatched'/'running' and the WHERE clause filters it out
--      so we cannot clobber an in-flight task.
-- Capped via LIMIT inside the CTE so a single sweep tick cannot monopolise
-- the DB when the backlog is large — the sweeper drains the rest on
-- subsequent ticks.
WITH victims AS (
    SELECT id FROM agent_task_queue
    WHERE status = 'queued'
      AND created_at < now() - make_interval(secs => @ttl_secs::double precision)
    ORDER BY created_at ASC
    LIMIT @max_per_tick::int
    FOR UPDATE SKIP LOCKED
)
UPDATE agent_task_queue t
SET status = 'failed',
    completed_at = now(),
    error = 'task expired in queue',
    failure_reason = 'queued_expired'
FROM victims v
WHERE t.id = v.id
  AND t.status = 'queued'
  AND t.created_at < now() - make_interval(secs => @ttl_secs::double precision)
RETURNING t.*;

-- name: CancelAgentTask :one
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: CountRunningTasks :one
SELECT count(*) FROM agent_task_queue
WHERE agent_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory');

-- name: HasActiveTaskForIssue :one
-- Returns true if there is any queued, dispatched, waiting_local_directory,
-- or running task for the issue.
SELECT count(*) > 0 AS has_active FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');

-- name: HasPendingTaskForIssue :one
-- Returns true if there is a queued or dispatched (but not yet running) task for the issue.
-- Used by the coalescing queue: allow enqueue when a task is running (so
-- the agent picks up new comments on the next cycle) but skip if a pending
-- task already exists (natural dedup).
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched');

-- name: HasPendingTaskForIssueAndAgent :one
-- Returns true if a specific agent already has a queued or dispatched task
-- for the given issue. Used by @mention trigger dedup.
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched');

-- name: GetLatestTaskIsLeaderForIssueAndAgent :one
-- Returns the is_leader_task flag of the agent's most recent task on this
-- issue, or NULL if the agent has never had a task on this issue. Used by
-- the squad-leader self-trigger guard to tell whether the agent's last
-- activity on the issue was in the leader role or the worker role (an
-- agent that holds both roles in a squad would otherwise be skipped by
-- the role-blind authorID == leaderID check).
SELECT is_leader_task FROM agent_task_queue
WHERE issue_id = $1 AND agent_id = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPendingTasksByRuntime :many
SELECT * FROM agent_task_queue
WHERE runtime_id = $1 AND status IN ('queued', 'dispatched')
ORDER BY priority DESC, created_at ASC;

-- name: ListQueuedClaimCandidatesByRuntime :many
-- Returns rows the runtime can attempt to claim. Status is restricted to
-- 'queued' (in contrast to ListPendingTasksByRuntime which also includes
-- 'dispatched') because dispatched rows are by definition already owned
-- and cannot be re-claimed — including them in the candidate list pads
-- the result with rows that always lose the per-(issue, agent) race in
-- ClaimAgentTask, wasting CPU and a SELECT every poll cycle when the
-- runtime is busy on a long-running task. Backed by the partial index
-- idx_agent_task_queue_claim_candidates so the warm path is cheap.
SELECT * FROM agent_task_queue
WHERE runtime_id = $1 AND status = 'queued'
ORDER BY priority DESC, created_at ASC;

-- name: ListActiveTasksByIssue :many
-- Backs the issue-detail "agent live" banner. Includes 'queued' so the
-- banner shows up the moment a task is enqueued — not only after a runtime
-- claims it. The queued window can be long when the runtime is offline or
-- busy on a prior task, and a silent UI during that window looks like the
-- platform never received the trigger.
SELECT * FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
ORDER BY created_at DESC;

-- name: GetWorkspaceAgentRunCounts :many
-- Total task runs per agent over the trailing 30 days, used by the Agents
-- list RUNS column. 30-day window keeps the count meaningful (a long-dormant
-- agent shouldn't show "5,420 runs from 2 years ago") and keeps the scan
-- bounded as the workspace ages.
SELECT
    atq.agent_id,
    COUNT(*)::int AS run_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.created_at > now() - INTERVAL '30 days'
GROUP BY atq.agent_id;

-- name: GetWorkspaceAgentActivity30d :many
-- Returns per-agent daily activity buckets for the last 30 days. Single
-- workspace-wide read backs both surfaces:
--   - Agents list ACTIVITY column — uses only the trailing 7 buckets
--   - Agent detail "Last 30 days" panel — uses the full 30
-- 30 days contains 7 days, so one fetch + a client-side .slice(-7) wins
-- over fetching twice. Days with no completion produce no row; the
-- front-end zero-fills.
--
-- Anchored on completed_at (not created_at) because the sparkline answers
-- "what did this agent produce?" not "what was queued at it?". A task that's
-- still in flight has no completed_at and contributes nothing here — that's
-- correct: in-flight tasks are surfaced via the live presence indicator,
-- not the historical trend.
SELECT
    atq.agent_id,
    DATE_TRUNC('day', atq.completed_at)::timestamptz AS bucket,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at > now() - INTERVAL '30 days'
GROUP BY atq.agent_id, bucket
ORDER BY atq.agent_id, bucket;

-- name: ListWorkspaceAgentTaskSnapshot :many
-- Returns the tasks needed to derive each agent's current presence:
--   - All active tasks (queued / dispatched / running) — for working signal + counts
--   - Each agent's most recent OUTCOME task (completed / failed) — for sticky
--     failed signal
-- The front-end picks "active wins, else latest outcome" — see derive-presence.ts.
--
-- Cancelled tasks are excluded from the outcome half on purpose: cancel is a
-- procedural signal ("attempt aborted"), not an outcome. It tells us nothing
-- about whether the agent works, so it must NOT be allowed to mask a prior
-- failure. Concretely: if an agent fails and then the user cancels the queued
-- retry (or the parent issue closes and cascades cancels), the failed signal
-- has to stay red. Only a real success (completed) or a fresh attempt (active)
-- clears it.
--
-- No UI windows in SQL: stickiness is decided by "is the latest outcome a
-- failure?", not a 2-minute clock. JOINs agent because agent_task_queue has
-- no workspace_id column.
SELECT atq.* FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')

UNION ALL

SELECT t.* FROM (
  SELECT DISTINCT ON (atq.agent_id) atq.*
  FROM agent_task_queue atq
  JOIN agent a ON a.id = atq.agent_id
  WHERE a.workspace_id = $1
    AND atq.status IN ('completed', 'failed')
  ORDER BY atq.agent_id, atq.completed_at DESC NULLS LAST
) t;

-- name: ListTasksByIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = $1
ORDER BY created_at DESC;

-- name: UpdateAgentStatus :one
UPDATE agent SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RefreshAgentStatusFromTasks :one
UPDATE agent AS a
SET status = CASE WHEN EXISTS (
    SELECT 1 FROM agent_task_queue q
    WHERE q.agent_id = a.id AND q.status IN ('dispatched', 'running', 'waiting_local_directory')
) THEN 'working' ELSE 'idle' END,
    updated_at = now()
WHERE a.id = $1
RETURNING *;
