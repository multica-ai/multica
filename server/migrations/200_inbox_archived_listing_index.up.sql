-- Back the archived-inbox listing (MUL-3736). The only inbox_item index is
-- idx_inbox_recipient (recipient_type, recipient_id, read), which carries
-- neither workspace_id nor archived nor created_at, so the archived list would
-- scan and sort every notification the recipient ever received. Keep this as
-- the migration's only statement: PostgreSQL rejects CREATE INDEX CONCURRENTLY
-- inside a transaction or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_inbox_recipient_archived_created
    ON inbox_item (workspace_id, recipient_type, recipient_id, archived, created_at DESC);
