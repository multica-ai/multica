CREATE INDEX CONCURRENTLY guardian_state_due_lease_idx ON guardian_state (state, next_wakeup, lease_expires_at) WHERE state IN ('pending', 'retry_wait', 'handoff_pending');
