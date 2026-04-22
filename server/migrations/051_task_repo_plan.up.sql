-- Repo planner: let tasks remember which repo they were routed to, and let
-- them pause while waiting for a user to disambiguate.

ALTER TABLE agent_task_queue
    ADD COLUMN target_repo_url TEXT,
    ADD COLUMN repo_confidence REAL;

ALTER TABLE agent_task_queue DROP CONSTRAINT agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled', 'awaiting_user'));
