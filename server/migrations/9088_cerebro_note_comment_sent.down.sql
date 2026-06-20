DROP INDEX IF EXISTS idx_cerebro_note_comment_unsent;
ALTER TABLE cerebro_note_comment DROP COLUMN IF EXISTS sent_to_agent_at;
