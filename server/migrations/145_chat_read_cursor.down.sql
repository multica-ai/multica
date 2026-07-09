ALTER TABLE chat_session ADD COLUMN unread_since TIMESTAMPTZ;

-- Best-effort restore of the boolean flag: any session with an assistant
-- message after its read cursor is stamped unread at the earliest such message.
UPDATE chat_session cs
   SET unread_since = sub.first_unread
  FROM (
    SELECT m.chat_session_id, min(m.created_at) AS first_unread
      FROM chat_message m
      JOIN chat_session s ON s.id = m.chat_session_id
     WHERE m.role = 'assistant' AND m.created_at > s.last_read_at
     GROUP BY m.chat_session_id
  ) sub
 WHERE cs.id = sub.chat_session_id;

ALTER TABLE chat_session DROP COLUMN last_read_at;
