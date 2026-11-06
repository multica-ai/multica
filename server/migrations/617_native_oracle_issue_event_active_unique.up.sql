-- Scope uniqueness to native opt-in Issue-event tasks only.
-- Existing manual/multi-agent tasks retain their original serialization rules.
-- The Issue table is the event backlog; no second queue table is introduced.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS
 idx_native_oracle_event_one_active_issue
 ON agent_task_queue(issue_id)
 WHERE context->>'oracle_issue_event'='true'
 AND status IN ('queued','dispatched','running','deferred','waiting_local_directory');