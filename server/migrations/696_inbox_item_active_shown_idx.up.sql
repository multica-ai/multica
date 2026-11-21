-- Orders a recipient's active inbox the way the list shows it: by when each
-- row arrived or came back (see returned_at in migration 695), newest
-- first. Serves ListInboxItems and the inbox limit's trim queries, which must
-- use this exact expression. Keep this as the migration's only statement:
-- PostgreSQL rejects CREATE INDEX CONCURRENTLY inside a transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_inbox_active_shown
    ON inbox_item (workspace_id, recipient_type, recipient_id, (GREATEST(created_at, returned_at)) DESC, id DESC)
    WHERE archived = false;
