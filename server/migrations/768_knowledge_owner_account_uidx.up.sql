CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_owner_account_uidx
    ON knowledge_owner_account (workspace_id, user_id);
