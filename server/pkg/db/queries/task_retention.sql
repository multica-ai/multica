-- Bounded agent task retention sweep (SE-37668). Deletes terminal history rows
-- from agent_task_queue that have aged past their status-specific retention
-- window: failed tasks after MULTICA_TASK_RETENTION_FAILED_DAYS and completed
-- tasks after MULTICA_TASK_RETENTION_COMPLETED_DAYS, both measured against the
-- DB completed_at timestamp.
--
-- The two statuses are queried separately, never OR'd together, so each query's
-- predicate implies migration 261's partial index
-- (idx_agent_task_queue_terminal_completed_at_v2, keyed on completed_at WHERE
-- status IN ('completed','failed','cancelled')) and the planner can range-scan
-- the window instead of scanning the whole lifetime table.
--
-- as_of is captured once per round by GetTaskRetentionAsOf and threaded into
-- every count and delete below, so a single round uses one consistent DB clock
-- for both the age cutoff and the 7-day autopilot recency window. Cutoffs are
-- strict (<): "older than N days", never "N days or older".
--
-- The @as_of parameter is parenthesised as (@as_of::timestamptz) wherever an
-- arithmetic operator follows it; sqlc miscomputes the named-parameter edit
-- span for a bare "@name::type - <expr>" and rejects the query otherwise.
--
-- Two autopilot guards protect a candidate regardless of age:
--   - direct  (autopilot_run.task_id = task.id): the task is a run's execution;
--   - reverse (autopilot_run.id = task.autopilot_run_id): the task belongs to a
--     run (retries inherit autopilot_run_id, so this also covers retry lineage).
-- Each guard fires when the run is still active (status IN
-- ('issue_created','running')) OR was created within the last 7 days, so an
-- active run is never reaped no matter how old its completed_at is, and any
-- recent run history is held for the recency window.
--
-- A child-lineage guard protects any task that still has a non-terminal child
-- (a retry or delegated sub-task), so an in-flight retry chain is never severed.

-- name: GetTaskRetentionAsOf :one
-- The single DB clock reading a whole sweep round shares for its age cutoff and
-- its autopilot recency window.
SELECT CAST(clock_timestamp() AS timestamptz) AS as_of;

-- name: CountFailedTaskRetentionCandidates :one
-- Exact count of failed tasks eligible for deletion as of @as_of. Used by the
-- dry-run summary and to decide whether another delete batch is warranted.
SELECT count(*) AS candidate_rows
FROM agent_task_queue t
WHERE t.status = 'failed'
  AND t.completed_at IS NOT NULL
  AND t.completed_at < (@as_of::timestamptz) - make_interval(days => @failed_days::int)
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.task_id = t.id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.id = t.autopilot_run_id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue child
      WHERE child.parent_task_id = t.id
        AND child.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  );

-- name: CountCompletedTaskRetentionCandidates :one
-- Exact count of completed tasks eligible for deletion as of @as_of.
SELECT count(*) AS candidate_rows
FROM agent_task_queue t
WHERE t.status = 'completed'
  AND t.completed_at IS NOT NULL
  AND t.completed_at < (@as_of::timestamptz) - make_interval(days => @completed_days::int)
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.task_id = t.id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.id = t.autopilot_run_id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue child
      WHERE child.parent_task_id = t.id
        AND child.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  );

-- name: DeleteFailedTaskRetentionBatch :many
-- Deletes one bounded batch of eligible failed tasks and returns their ids so
-- the sweeper can count actual deletions. The victims CTE takes FOR UPDATE OF t
-- SKIP LOCKED so a row another transaction is touching is skipped, not blocked
-- on; the DELETE re-checks every predicate at apply time so a row that changed
-- state between selection and delete is filtered out rather than clobbered.
-- Deterministic order (completed_at ASC, id ASC) drains the oldest rows first
-- and makes a rerun idempotent.
WITH victims AS (
    SELECT t.id
    FROM agent_task_queue t
    WHERE t.status = 'failed'
      AND t.completed_at IS NOT NULL
      AND t.completed_at < (@as_of::timestamptz) - make_interval(days => @failed_days::int)
      AND NOT EXISTS (
          SELECT 1 FROM autopilot_run ar
          WHERE ar.task_id = t.id
            AND (ar.status IN ('issue_created', 'running')
                 OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
      )
      AND NOT EXISTS (
          SELECT 1 FROM autopilot_run ar
          WHERE ar.id = t.autopilot_run_id
            AND (ar.status IN ('issue_created', 'running')
                 OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
      )
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue child
          WHERE child.parent_task_id = t.id
            AND child.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
      )
    ORDER BY t.completed_at ASC, t.id ASC
    LIMIT @batch_size::int
    FOR UPDATE OF t SKIP LOCKED
)
DELETE FROM agent_task_queue d
USING victims v
WHERE d.id = v.id
  AND d.status = 'failed'
  AND d.completed_at IS NOT NULL
  AND d.completed_at < (@as_of::timestamptz) - make_interval(days => @failed_days::int)
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.task_id = d.id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.id = d.autopilot_run_id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue child
      WHERE child.parent_task_id = d.id
        AND child.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  )
RETURNING d.id;

-- name: DeleteCompletedTaskRetentionBatch :many
-- Deletes one bounded batch of eligible completed tasks. Same locking, re-check
-- and ordering guarantees as DeleteFailedTaskRetentionBatch.
WITH victims AS (
    SELECT t.id
    FROM agent_task_queue t
    WHERE t.status = 'completed'
      AND t.completed_at IS NOT NULL
      AND t.completed_at < (@as_of::timestamptz) - make_interval(days => @completed_days::int)
      AND NOT EXISTS (
          SELECT 1 FROM autopilot_run ar
          WHERE ar.task_id = t.id
            AND (ar.status IN ('issue_created', 'running')
                 OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
      )
      AND NOT EXISTS (
          SELECT 1 FROM autopilot_run ar
          WHERE ar.id = t.autopilot_run_id
            AND (ar.status IN ('issue_created', 'running')
                 OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
      )
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue child
          WHERE child.parent_task_id = t.id
            AND child.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
      )
    ORDER BY t.completed_at ASC, t.id ASC
    LIMIT @batch_size::int
    FOR UPDATE OF t SKIP LOCKED
)
DELETE FROM agent_task_queue d
USING victims v
WHERE d.id = v.id
  AND d.status = 'completed'
  AND d.completed_at IS NOT NULL
  AND d.completed_at < (@as_of::timestamptz) - make_interval(days => @completed_days::int)
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.task_id = d.id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM autopilot_run ar
      WHERE ar.id = d.autopilot_run_id
        AND (ar.status IN ('issue_created', 'running')
             OR ar.created_at >= (@as_of::timestamptz) - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue child
      WHERE child.parent_task_id = d.id
        AND child.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  )
RETURNING d.id;
