-- Channel names are unique in a workspace.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_channel_name_uidx
    ON conversation (workspace_id, lower(name))
    WHERE kind = 'channel';
