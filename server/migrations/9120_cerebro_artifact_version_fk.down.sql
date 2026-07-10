-- Revert the FK back to cerebro_note(artifact_id). This only succeeds if every
-- note_id currently points at an artifact that also has a cerebro_note row
-- (i.e. no document versions exist yet); otherwise the constraint add fails,
-- which is the intended safety behavior. Mirrors 9089's down migration.
ALTER TABLE cerebro_note_version
    DROP CONSTRAINT IF EXISTS cerebro_note_version_note_id_fkey;

ALTER TABLE cerebro_note_version
    ADD CONSTRAINT cerebro_note_version_note_id_fkey
    FOREIGN KEY (note_id) REFERENCES cerebro_note(artifact_id) ON DELETE CASCADE;
