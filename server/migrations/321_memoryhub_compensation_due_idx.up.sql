CREATE INDEX CONCURRENTLY memoryhub_compensation_due_idx ON memoryhub_compensation (state, next_attempt_at) WHERE state IN ('pending', 'retry_wait');
