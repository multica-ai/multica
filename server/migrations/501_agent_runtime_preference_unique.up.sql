CREATE UNIQUE INDEX CONCURRENTLY agent_runtime_preference_identity_idx ON agent_runtime_preference (workspace_id, user_id, agent_id);
