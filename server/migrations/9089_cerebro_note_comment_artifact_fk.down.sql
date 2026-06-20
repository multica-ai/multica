-- Revert the FK back to cerebro_note(artifact_id). This only succeeds if every
-- note_id currently points at an artifact that also has a cerebro_note row
-- (i.e. no document comments exist yet); otherwise the constraint add fails,
-- which is the intended safety behavior.
ALTER TABLE cerebro_note_comment
    DROP CONSTRAINT IF EXISTS cerebro_note_comment_note_id_fkey;

ALTER TABLE cerebro_note_comment
    ADD CONSTRAINT cerebro_note_comment_note_id_fkey
    FOREIGN KEY (note_id) REFERENCES cerebro_note(artifact_id) ON DELETE CASCADE;

ALTER TABLE cerebro_note_reference
    DROP CONSTRAINT IF EXISTS cerebro_note_reference_note_id_fkey;

ALTER TABLE cerebro_note_reference
    ADD CONSTRAINT cerebro_note_reference_note_id_fkey
    FOREIGN KEY (note_id) REFERENCES cerebro_note(artifact_id) ON DELETE CASCADE;
