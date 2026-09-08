-- Comment replies no longer escalate to the issue assignee. Retire old
-- fallback tasks that have not started; preserve running tasks and history.
-- Stop old API instances before running this one-time cleanup: they can still
-- enqueue new fallback rows after it finishes. The Helm Recreate deployment
-- and migrate-before-server entrypoint provide this ordering.
WITH cancelled AS (
    UPDATE agent_task_queue
    SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
    WHERE escalation_for_task_id IS NOT NULL
      AND started_at IS NULL
      AND status IN ('deferred', 'queued', 'dispatched', 'waiting_local_directory')
    RETURNING id, agent_id
), desired AS (
    SELECT DISTINCT cancelled.agent_id,
        CASE WHEN EXISTS (
            SELECT 1 FROM agent_task_queue task
            WHERE task.agent_id = cancelled.agent_id
              AND task.status IN ('dispatched', 'running')
              -- Data-modifying CTEs share a snapshot. Exclude the returned
              -- rows explicitly rather than reading their old task status.
              AND NOT EXISTS (SELECT 1 FROM cancelled c WHERE c.id = task.id)
        ) THEN 'working' ELSE 'idle' END AS status
    FROM cancelled
)
UPDATE agent
SET status = desired.status, updated_at = now()
FROM desired
WHERE agent.id = desired.agent_id AND agent.status IS DISTINCT FROM desired.status;
