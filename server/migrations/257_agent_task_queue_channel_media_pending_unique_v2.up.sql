-- Build v2 before dropping v1 so queued/dispatched uniqueness remains enforced
-- throughout rolling deploys and interrupted migration runs. The v1 predicate
-- is a strict subset of v2, so both indexes can coexist safely. This file stays
-- a single statement because CREATE INDEX CONCURRENTLY cannot run in a
-- transaction or a multi-command string.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_one_pending_task_per_issue_agent_v2
    ON agent_task_queue (issue_id, agent_id)
    WHERE status IN ('queued', 'dispatched')
       OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true');
