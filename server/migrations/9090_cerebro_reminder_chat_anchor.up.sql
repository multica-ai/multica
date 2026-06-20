-- CEREBRO-PATCH(cerebro-reminder): FIR-394 — chat-message anchor.
--
-- The unified reminder could anchor to a comment / issue / project / nothing,
-- but NOT to a chat message — a chat message lives in chat_message, a different
-- table than comment, so the existing 'comment' anchor (message_id → comment)
-- can't point at it. This adds a first-class chat-message anchor so "remind me
-- about THIS chat message" (set from the message's 3-dot menu) works like every
-- other surface: it shows in the unified overview and re-fires by re-surfacing
-- the chat in the inbox (the sweeper stamps chat_session.unread_since).
ALTER TABLE cerebro_reminder
    ADD COLUMN IF NOT EXISTS chat_message_id UUID
        REFERENCES chat_message(id) ON DELETE SET NULL;

ALTER TABLE cerebro_reminder
    DROP CONSTRAINT IF EXISTS cerebro_reminder_anchor_type_check,
    ADD  CONSTRAINT cerebro_reminder_anchor_type_check
         CHECK (anchor_type IN ('comment', 'issue', 'project', 'chat_message', 'none'));
