-- Reverts daemon-session fencing state.
ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS daemon_session_id;

ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS daemon_session_id;
