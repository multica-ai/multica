-- Rolling gate: deploy v3-aware duplicate handling to every server and drain
-- v2-only binaries before applying this migration or enabling Pool writers.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_one_pending_task_per_issue_agent_v3
    ON agent_task_queue (issue_id, agent_id)
    WHERE status IN ('queued', 'dispatched')
       OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true')
       OR status = 'waiting_runtime';
