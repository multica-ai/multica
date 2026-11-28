-- Upsert key for the per-user per-workspace recent agent order.
CREATE UNIQUE INDEX CONCURRENTLY idx_recent_agent_selection_workspace_user ON recent_agent_selection (workspace_id, user_id);
