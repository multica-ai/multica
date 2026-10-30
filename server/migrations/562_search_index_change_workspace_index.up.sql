-- Serves the catch-up read: one workspace, an xid range, ordered by the keyset
-- (change_xid, entity_type, entity_id) the client pages through.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_search_index_change_workspace_xid
    ON search_index_change (workspace_id, change_xid, entity_type, entity_id);
