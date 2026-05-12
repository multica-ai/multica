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
