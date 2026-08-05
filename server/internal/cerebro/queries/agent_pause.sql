-- Cerebro-only agent pause/unpause queries (FIR-4508). Operate on the
-- upstream agent and agent_task_queue tables; pause columns are added by
-- 9171_cerebro_agent_pause.

-- name: PauseAgentRow :one
-- Marks an agent paused. Idempotent: re-pausing updates unpause_at /
-- pause_reason but does not reset paused_at.
UPDATE agent
SET paused_at    = COALESCE(paused_at, now()),
    unpause_at   = $2,
    pause_reason = $3,
    updated_at   = now()
WHERE id = $1
RETURNING *;

-- name: UnpauseAgentRow :one
-- Clears all pause fields. Idempotent on already-unpaused agents.
UPDATE agent
SET paused_at    = NULL,
    unpause_at   = NULL,
    pause_reason = NULL,
    updated_at   = now()
WHERE id = $1
RETURNING *;

-- name: IncrementAgentAutoPauseCount :one
-- Circuit breaker counter for agent-scoped auto-pause (FIR-4508).
UPDATE agent
SET auto_pause_count = auto_pause_count + 1,
    updated_at       = now()
WHERE id = $1
RETURNING auto_pause_count;

-- name: ResetAgentAutoPauseCount :exec
-- Clear the consecutive agent auto-pause counter after a successful task.
UPDATE agent
SET auto_pause_count = 0,
    updated_at       = now()
WHERE id = $1
  AND auto_pause_count > 0;

-- name: GetAgentPauseSnapshot :one
-- Snapshot pause fields before UnpauseAgentRow clears them.
SELECT id, paused_at, unpause_at, pause_reason, auto_pause_count, owner_id, workspace_id, name, runtime_id
FROM agent
WHERE id = $1;

-- name: ListAgentsDueForUnpause :many
-- Agent unpause sweeper: scheduled unpause_at has passed.
SELECT * FROM agent
WHERE paused_at IS NOT NULL
  AND unpause_at IS NOT NULL
  AND unpause_at <= now();

-- name: StampQueuedTasksAgentPauseWaitReason :exec
-- Explain queued waits on the task row (agent-scoped pause).
UPDATE agent_task_queue
SET wait_reason = $2
WHERE agent_id = $1
  AND status = 'queued';

-- name: ClearQueuedTasksAgentPauseWaitReason :exec
-- Clears agent-pause stamps on unpause. Scoped to our prefix.
UPDATE agent_task_queue
SET wait_reason = NULL
WHERE agent_id = $1
  AND status = 'queued'
  AND wait_reason LIKE 'agent_paused|%';

-- name: SuspendActiveTasksForAgent :many
-- Called when an agent is paused: mark in-flight work failed with
-- failure_reason='agent_paused' so the agent-unpause path can resume it.
UPDATE agent_task_queue
SET status         = 'failed',
    completed_at   = now(),
    error          = 'agent paused',
    failure_reason = 'agent_paused'
WHERE agent_id = $1
  AND status IN ('dispatched', 'running')
RETURNING *;

-- name: ListResumableTasksForAgent :many
-- Called on agent unpause. Two categories:
--   1. failure_reason='agent_paused' — interrupted by PauseAgent
--   2. Transient auth/quota failures in the 10 minutes before paused_at
-- Leaf-only so already-resumed parents are skipped.
SELECT t.* FROM agent_task_queue t
WHERE t.agent_id = $1
  AND t.status = 'failed'
  AND (
        t.failure_reason = 'agent_paused'
        OR (
              sqlc.narg('paused_at')::timestamptz IS NOT NULL
              AND t.failure_reason IN (
                    'rate_limit',
                    'auth_error',
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
