CREATE INDEX CONCURRENTLY memoryhub_memory_item_active_expiry_idx ON memoryhub_memory_item (state, expires_at) WHERE state = 'active';
