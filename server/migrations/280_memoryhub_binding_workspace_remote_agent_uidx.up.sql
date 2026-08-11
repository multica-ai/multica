CREATE UNIQUE INDEX CONCURRENTLY memoryhub_binding_workspace_remote_agent_uidx ON memoryhub_binding (workspace_id, remote_agent_id) WHERE scope_kind = 'workspace' AND remote_agent_id IS NOT NULL;
