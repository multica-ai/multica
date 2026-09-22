CREATE UNIQUE INDEX CONCURRENTLY workflow_chat_identity_idx ON workflow_chat (workflow_id, user_id, agent_id);
