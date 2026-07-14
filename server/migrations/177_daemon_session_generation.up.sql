-- A daemon process receives a new session generation at startup. The runtime
-- records the current owner and each dispatched task snapshots that owner, so
-- recovery can prove a task belongs to an older process before it is failed.
ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS daemon_session_id UUID;

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS daemon_session_id UUID;
