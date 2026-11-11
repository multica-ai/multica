-- Requests of one run: duplicate detection and the chat view.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_action_request_task
    ON app_action_request (task_id);
