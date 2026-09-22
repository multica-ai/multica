CREATE UNIQUE INDEX CONCURRENTLY workflow_chat_turn_idempotency_idx
ON workflow_chat_turn (workspace_id, user_id, workflow_id, idempotency_key)
WHERE idempotency_key IS NOT NULL;
