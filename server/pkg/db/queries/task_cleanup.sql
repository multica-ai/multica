-- name: TaskBlockedByProcessCleanup :one
-- A cleanup failure fences this agent on this runtime, including already queued
-- work and future scheduled runs. Only a NEW explicit manual rerun can cross
-- the fence; its successful completion allows later automated work again.
-- Existing failure and result fields are the durable safety markers.
SELECT EXISTS (
    SELECT 1
    FROM agent_task_queue candidate
    JOIN LATERAL (
        SELECT CASE WHEN unsafe.status = 'cancelled'
                    AND unsafe.result->>'process_cleanup_pending' = 'true'
               THEN unsafe.completed_at + interval '30 seconds'
               ELSE COALESCE(unsafe.completed_at, unsafe.created_at)
               END AS completed_at
        FROM agent_task_queue unsafe
        WHERE unsafe.agent_id = candidate.agent_id
          AND unsafe.runtime_id = candidate.runtime_id
          AND (unsafe.failure_reason = 'cleanup_unconfirmed'
            OR (unsafe.status = 'cancelled' AND unsafe.result->>'process_cleanup_pending' = 'true'
                AND unsafe.completed_at <= now() - interval '30 seconds'))
        ORDER BY unsafe.completed_at DESC, unsafe.id DESC
        LIMIT 1
    ) unsafe ON true
    WHERE candidate.id = @task_id
      AND candidate.status IN ('queued', 'dispatched', 'waiting_local_directory')
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue manual
          WHERE manual.agent_id = candidate.agent_id
            AND manual.runtime_id = candidate.runtime_id
            AND manual.rerun_of_task_id IS NOT NULL
            AND manual.retry_of_task_id IS NULL
            AND manual.created_at > unsafe.completed_at
            AND (manual.id = candidate.id OR manual.status = 'completed')
      )
);

-- name: FailCancelledTaskProcessCleanup :one
-- The daemon's cancellation acknowledgement can discover that local work did
-- NOT stop. Preserve the cancelled run's metadata but surface this as failure.
-- The status CAS makes replay harmless and leaves completed/failed rows alone.
UPDATE agent_task_queue
SET status = 'failed',
    error = concat_ws('; ', NULLIF(error, ''), @error_message::text),
    failure_reason = 'cleanup_unconfirmed',
    completed_at = now(),
    chat_finalize_deferred_at = NULL
WHERE id = @id AND status = 'cancelled'
RETURNING *;

-- name: MarkPiTaskProcessCleanupPending :exec
-- Cancellation makes the task terminal before the daemon finishes stopping it.
-- Only a run that negotiated confirmation at start holds a fence until ack.
UPDATE agent_task_queue task
SET result = COALESCE(task.result, '{}'::jsonb) || '{"process_cleanup_pending":true}'::jsonb
WHERE task.id = @id AND task.status = 'cancelled' AND task.started_at IS NOT NULL
  AND task.result->>'process_cleanup_capability' = 'pi-process-cleanup-v1'
  AND EXISTS (SELECT 1 FROM agent_runtime runtime WHERE runtime.id = task.runtime_id AND runtime.provider IN ('pi', 'omp'));

-- name: ClearTaskProcessCleanupPending :exec
UPDATE agent_task_queue
SET result = result - 'process_cleanup_pending'
WHERE id = @id AND status IN ('cancelled', 'failed')
  AND result->>'process_cleanup_pending' = 'true';

-- name: TaskWaitingForProcessCleanup :one
SELECT EXISTS (
    SELECT 1 FROM agent_task_queue candidate
    JOIN agent_task_queue stopping ON stopping.agent_id = candidate.agent_id AND stopping.runtime_id = candidate.runtime_id
    WHERE candidate.id = @id AND candidate.status IN ('dispatched', 'waiting_local_directory')
      AND stopping.status = 'cancelled' AND stopping.result->>'process_cleanup_pending' = 'true'
      AND stopping.completed_at > now() - interval '30 seconds'
);
