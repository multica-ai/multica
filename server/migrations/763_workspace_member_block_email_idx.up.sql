CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_member_block_email ON workspace_member_block (workspace_id, email);
