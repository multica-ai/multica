-- TECH-3352 — per-chat-session snooze ("remind me"), aligning chat rows with
-- issue/channel/DM rows in the inbox. Chat sessions are per-user (creator_id),
-- so the snooze lives directly on the row. A future timestamp hides the chat
-- from the normal inbox views until it passes — same contract as the channel/DM
-- muted_until. Mark-as-unread already exists (chat_session.unread_since).
ALTER TABLE chat_session ADD COLUMN IF NOT EXISTS muted_until timestamptz;
