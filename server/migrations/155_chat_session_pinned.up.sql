-- Per-conversation pin for the Chat list: a user can pin a chat so it stays
-- at the top of their conversation list, above the activity-sorted rest.
-- `pinned_at` doubles as the sort key within the pinned group (most-recently
-- pinned first) and as the boolean flag (NULL = not pinned). Sessions are
-- already per-creator, so no extra user dimension is needed.
ALTER TABLE chat_session ADD COLUMN IF NOT EXISTS pinned_at TIMESTAMPTZ;
