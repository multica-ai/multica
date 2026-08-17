-- Partial index over pinned rows only: the pinned group is small, and the list
-- query orders by pinned_at within a single (workspace, creator) scan.
--
-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_session_pinned
    ON chat_session (creator_id, workspace_id, pinned_at DESC)
    WHERE pinned_at IS NOT NULL;
