-- FIR-1621 — unify documents into the comments model. A note comment used to FK
-- cerebro_note(artifact_id), which limited comments to notes (kind='note'). The
-- product decision (Jesper) is that ANY document — a report/plan/decision/diagram
-- artifact, not just a personal note — can carry comments through the same panel.
--
-- A cerebro_note row is just an artifact with note-state attached, so every
-- existing note_id already IS an artifact.id; repointing the FK to artifact(id)
-- is a strict superset and leaves all existing comment rows valid. Access control
-- still differs per shape (notes apply their visibility rule, documents apply
-- folder access) — that lives in the handler (CanUserCommentOnArtifact), not here.
ALTER TABLE cerebro_note_comment
    DROP CONSTRAINT IF EXISTS cerebro_note_comment_note_id_fkey;

ALTER TABLE cerebro_note_comment
    ADD CONSTRAINT cerebro_note_comment_note_id_fkey
    FOREIGN KEY (note_id) REFERENCES artifact(id) ON DELETE CASCADE;

-- Same move for references: a document must be couplable to an issue/chat (the
-- "send comments to the agent" destination), so its note_id reference must also
-- accept a plain artifact id, not only a cerebro_note row.
ALTER TABLE cerebro_note_reference
    DROP CONSTRAINT IF EXISTS cerebro_note_reference_note_id_fkey;

ALTER TABLE cerebro_note_reference
    ADD CONSTRAINT cerebro_note_reference_note_id_fkey
    FOREIGN KEY (note_id) REFERENCES artifact(id) ON DELETE CASCADE;
