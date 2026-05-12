-- Cerebro-only runtime pause/unpause queries. Operate on the upstream
-- agent_runtime and agent_task_queue tables; the pause-related columns
-- (paused_at, unpause_at, pause_reason) are added by 9016_cerebro_runtime_pause.

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

-- name: GetAgentRuntimePauseSnapshot :one
-- Reads the current pause fields for a runtime. Used by UnpauseRuntime to
-- capture paused_at *before* clearing it, so the resume-task selection can
-- key on the original pause-start timestamp (the resume window is anchored
-- on when the pause began, not when it ends).
SELECT id, paused_at, unpause_at, pause_reason
FROM agent_runtime
WHERE id = $1;

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
-- Called on unpause. Returns leaf tasks on this runtime that the unpause
-- path should resume. Two categories:
--
--   1. failure_reason='runtime_paused' — tasks the pause itself interrupted
--      (suspended via SuspendActiveTasksForRuntime when pause kicked in).
--      Returned unconditionally; the leaf-only filter excludes pause episodes
--      that have already been resumed (descendant retry exists).
--
--   2. Transient-error tasks ('rate_limit','runtime_offline','runtime_recovery',
--      'timeout') that failed in the 10 minutes immediately before paused_at.
--      These are the tasks whose failures triggered the pause — typically 1-3
--      tasks that hit the provider rate-limit or expired token before
--      SuspendActiveTasks could suspend the rest. Older transient failures
--      are by definition unrelated to *this* pause episode and must not be
--      resumed by it.
--
-- $2 is the pause-start timestamp (captured by Go via
-- GetAgentRuntimePauseSnapshot before UnpauseAgentRuntime clears it). If
-- $2 IS NULL the caller could not determine when the pause started — fall
-- back to category 1 only, which keeps idempotent unpause-of-unpaused safe.
--
-- "Leaf" means no descendant retry already exists, preventing duplicate
-- resumption from manual reruns, autopilot ticks, or prior unpauses.
SELECT t.* FROM agent_task_queue t
WHERE t.runtime_id = $1
  AND t.status = 'failed'
  AND (
        t.failure_reason = 'runtime_paused'
        OR (
              sqlc.narg('paused_at')::timestamptz IS NOT NULL
              AND t.failure_reason IN (
                    'rate_limit',
                    'runtime_offline',
                    'runtime_recovery',
                    'timeout'
                  )
              AND t.completed_at >= sqlc.narg('paused_at')::timestamptz - INTERVAL '10 minutes'
              AND t.completed_at <  sqlc.narg('paused_at')::timestamptz
            )
      )
  AND NOT EXISTS (
        SELECT 1 FROM agent_task_queue d WHERE d.parent_task_id = t.id
      )
ORDER BY t.completed_at ASC;

-- name: CreateResumeFromPauseTask :one
-- Like CreateRetryTask but resets the attempt counter to 1. Used when
-- the unpause sweeper resumes work that died during a rate-limit pause —
-- the pause was caused by an external blocker, not by the task itself, so
-- giving it a fresh max_attempts budget is correct. Carries forward the
-- agent's session context so the conversation continues seamlessly.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, chat_session_id, autopilot_run_id,
    status, priority, trigger_comment_id, trigger_summary, context,
    session_id, work_dir,
    attempt, max_attempts, parent_task_id
)
SELECT
    p.agent_id, p.runtime_id, p.issue_id, p.chat_session_id, p.autopilot_run_id,
    'queued', p.priority, p.trigger_comment_id, p.trigger_summary, p.context,
    p.session_id, p.work_dir,
    1, p.max_attempts, p.id
FROM agent_task_queue p
WHERE p.id = $1
RETURNING *;
