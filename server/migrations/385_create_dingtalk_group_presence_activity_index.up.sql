CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_dingtalk_group_presence_workspace_activity ON dingtalk_group_presence(workspace_id, last_active_at DESC NULLS LAST, installation_id, conversation_id);
