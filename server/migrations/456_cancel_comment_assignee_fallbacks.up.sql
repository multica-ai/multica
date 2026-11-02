-- Comment replies no longer escalate to the issue assignee. Retire old
-- fallback tasks and their automatic retries that have not started; preserve
-- running tasks, completed history, and user-requested reruns.
-- Stop old API instances before running this one-time cleanup: they can still
-- enqueue new fallback rows after it finishes. The Helm Recreate deployment
-- and migrate-before-server entrypoint provide this ordering.
WITH RECURSIVE fallback_lineage AS (
    SELECT task.id AS task_id,
        task.escalation_for_task_id,
        ARRAY[task.id] AS path
    FROM agent_task_queue task
    WHERE task.escalation_for_task_id IS NOT NULL
      -- CreateDeferredAgentTask never set retry_of_task_id. Excluding retry
      -- descendants backfilled by an earlier idempotent run keeps one root per
      -- lineage instead of rediscovering every suffix as a separate tree.
      AND task.retry_of_task_id IS NULL

    UNION ALL

    -- CreateRetryTask stores both links to the same immediate parent but does
    -- not copy escalation_for_task_id. Following the indexed parent_task_id
    -- finds descendants without scanning the whole queue; retry_of_task_id
    -- keeps user-requested reruns outside this cleanup.
    SELECT retry.id,
        lineage.escalation_for_task_id,
        lineage.path || retry.id
    FROM fallback_lineage lineage
    JOIN agent_task_queue retry
      ON retry.parent_task_id = lineage.task_id
     AND retry.retry_of_task_id = lineage.task_id
    -- Stored retry links should be acyclic, but corrupt historical data must
    -- not make a startup migration recurse forever.
    WHERE NOT retry.id = ANY(lineage.path)
), cancelled AS (
    UPDATE agent_task_queue task
    SET status = 'cancelled',
        completed_at = now(),
        prepare_lease_expires_at = NULL,
        escalation_for_task_id = fallback_lineage.escalation_for_task_id
    FROM fallback_lineage
    WHERE task.id = fallback_lineage.task_id
      AND task.started_at IS NULL
      -- An unstarted fallback may have absorbed newer comments while queued.
      -- Preserve it so those merged instructions still receive their run.
      AND cardinality(task.coalesced_comment_ids) = 0
      AND task.status IN ('deferred', 'queued', 'dispatched', 'waiting_local_directory')
    RETURNING task.id, task.agent_id
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
