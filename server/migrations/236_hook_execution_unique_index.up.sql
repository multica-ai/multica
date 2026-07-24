CREATE UNIQUE INDEX CONCURRENTLY idx_hook_execution_hook_event ON hook_execution (hook_id, event_id);
