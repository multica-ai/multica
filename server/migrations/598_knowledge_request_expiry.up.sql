CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_request_expiry_idx ON knowledge_request (expires_at, status);
