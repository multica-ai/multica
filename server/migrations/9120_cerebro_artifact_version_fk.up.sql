-- FIR-2697 — extend version history from personal Notes to ALL agent-created
-- documents. cerebro_note_version.note_id used to FK cerebro_note(artifact_id),
-- which limited version history to notes (an artifact with a cerebro_note row).
-- The product decision (Jesper) is that ANY agent-created document — a
-- report/plan/decision/diagram/note artifact — accumulates version history, so
-- an update makes a new version of the same file instead of a new file.
--
-- A cerebro_note row is just an artifact with note-state attached, so every
-- existing note_id already IS an artifact.id; repointing the FK to artifact(id)
-- is a strict superset and leaves all existing version rows valid. The snapshot
-- logic already operates purely on the artifact's title/body (see
-- server/internal/cerebro/note/versions.go: snapshotVersion) — it never reads a
-- cerebro_note field — so no data changes, only the FK target widens. Mirrors
-- the same move made for note comments/references in 9089.
--
-- Real deployments hold cerebro_note rows that outlived their artifact row
-- (hard-deleted artifacts never cascaded into cerebro_note, which has no FK to
-- artifact), so version rows can reference a note_id with no artifact behind
-- it. Those versions belong to deleted files and are unreachable from any
-- surface — drop them first or the new FK cannot be added.
DELETE FROM cerebro_note_version v
    WHERE NOT EXISTS (SELECT 1 FROM artifact a WHERE a.id = v.note_id);

ALTER TABLE cerebro_note_version
    DROP CONSTRAINT IF EXISTS cerebro_note_version_note_id_fkey;

ALTER TABLE cerebro_note_version
    ADD CONSTRAINT cerebro_note_version_note_id_fkey
    FOREIGN KEY (note_id) REFERENCES artifact(id) ON DELETE CASCADE;
