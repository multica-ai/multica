-- FIR-1621 — agent collaboration on notes/documents. A note comment is a draft
-- that the author accumulates locally and then explicitly *sends to an agent*
-- (there is no auto-fire on @-mention inside a note — sending is a deliberate
-- action). This column records that moment:
--
--   sent_to_agent_at IS NULL   -> unsent: still a local draft comment,
--   sent_to_agent_at = <ts>    -> dispatched to the note's coupled destination
--                                 (an issue or a chat — see cerebro_note_reference).
--
-- The UI uses it to mark which comments are not yet sent and to offer
-- "send all" / "send selected". Sending is idempotent: an already-sent comment
-- keeps its original timestamp.
ALTER TABLE cerebro_note_comment
    ADD COLUMN IF NOT EXISTS sent_to_agent_at TIMESTAMPTZ;

-- Partial index for the hot read "list the unsent comments on this note".
CREATE INDEX IF NOT EXISTS idx_cerebro_note_comment_unsent
    ON cerebro_note_comment (note_id)
    WHERE sent_to_agent_at IS NULL;
