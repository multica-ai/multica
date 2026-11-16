-- IM-style unread: keep the old `unread_since` flag for rolling deploy /
-- rollback compatibility while adding a read cursor `last_read_at`. Unread is
-- now derived as the *count* of assistant messages after the cursor, so the chat
-- list can show a real number (like an IM conversation) instead of a single dot.
ALTER TABLE chat_session ADD COLUMN IF NOT EXISTS unread_since TIMESTAMPTZ;
ALTER TABLE chat_session ADD COLUMN IF NOT EXISTS last_read_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Preserve existing unread state during the switch: a session currently flagged
-- unread gets its cursor placed just before the first unread reply, so those
-- messages still count. `unread_since` is the arrival time of the first unread
-- assistant message.
UPDATE chat_session
   SET last_read_at = unread_since - interval '1 microsecond'
 WHERE unread_since IS NOT NULL;
