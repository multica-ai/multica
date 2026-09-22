CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_request_scope_idempotency_uidx ON knowledge_request (workspace_id, actor_key, operation, idempotency_key);
