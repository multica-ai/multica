CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS member_search_access_pkey_idx ON member_search_access (workspace_id, user_id);
