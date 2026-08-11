CREATE INDEX CONCURRENTLY memoryhub_secret_state_lease_idx ON memoryhub_secret (state, lease_expires_at) WHERE state IN ('rotating', 'blocked_migration');
