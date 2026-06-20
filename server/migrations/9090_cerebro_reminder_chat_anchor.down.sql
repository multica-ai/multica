-- CEREBRO-PATCH(cerebro-reminder): FIR-394 chat-message anchor — down. Drop the
-- chat-anchored reminders first (the narrower check can't represent them), then
-- remove the column and revert the anchor_type check.
DELETE FROM cerebro_reminder WHERE anchor_type = 'chat_message';

ALTER TABLE cerebro_reminder
    DROP CONSTRAINT IF EXISTS cerebro_reminder_anchor_type_check,
    ADD  CONSTRAINT cerebro_reminder_anchor_type_check
         CHECK (anchor_type IN ('comment', 'issue', 'project', 'none'));

ALTER TABLE cerebro_reminder
    DROP COLUMN IF EXISTS chat_message_id;
