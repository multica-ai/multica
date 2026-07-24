CREATE INDEX CONCURRENTLY idx_hook_execution_lease ON hook_execution (status, next_attempt_at, lease_expires_at, created_at);
