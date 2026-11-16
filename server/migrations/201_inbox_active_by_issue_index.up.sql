-- Partial index over ACTIVE rows keyed by issue (MUL-3736). Serves the
-- NOT EXISTS probe in ListArchivedInboxItems, which asks "does this issue still
-- have a non-archived row?" once per archived row, and the identically-filtered
-- ArchiveInboxByIssue / UnarchiveInboxByIssue writes. Partial on archived=false
-- so it stays small as the archive grows. Keep this as the migration's only
-- statement: PostgreSQL rejects CREATE INDEX CONCURRENTLY inside a transaction
-- or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_inbox_active_by_issue
    ON inbox_item (workspace_id, recipient_type, recipient_id, issue_id)
    WHERE archived = false;
