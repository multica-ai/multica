-- Per-conversation pin for the Chat list: a user can pin a chat so it stays
-- at the top of their conversation list, above the activity-sorted rest.
-- `pinned_at` doubles as the sort key within the pinned group (most-recently
-- pinned first) and as the boolean flag (NULL = not pinned). Sessions are
-- already per-creator, so no extra user dimension is needed.
ALTER TABLE chat_session ADD COLUMN pinned_at TIMESTAMPTZ;

-- Partial index over pinned rows only — the pinned group is small, and the
-- list query orders by pinned_at within a single (workspace, creator) scan.
CREATE INDEX idx_chat_session_pinned
    ON chat_session (creator_id, workspace_id, pinned_at DESC)
    WHERE pinned_at IS NOT NULL;
