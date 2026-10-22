-- Drain per-thread pending queues before downgrading; never discard requests
-- merely to recreate the older, narrower uniqueness constraint.
CREATE UNIQUE INDEX CONCURRENTLY idx_one_pending_task_per_issue_agent_v2
    ON agent_task_queue (issue_id, agent_id)
    WHERE status IN ('queued', 'dispatched')
       OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true');
