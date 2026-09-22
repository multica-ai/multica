CREATE UNIQUE INDEX CONCURRENTLY workflow_command_identity_idx
ON workflow_command (workspace_id, actor_id, operation, resource_id, idempotency_key);
