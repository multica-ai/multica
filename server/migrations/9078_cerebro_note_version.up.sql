-- Wave 3 / G2 (TECH-3556) — version history for a Note.
--
-- Every meaningful save snapshots the note's title+body so a user can see who
-- changed what and when, and restore an earlier state. The note's *live*
-- content still lives on the underlying artifact (UpdateNote writes it there);
-- this table is the append-only history beside it.
--
--   SNAPSHOT STRATEGY — session coalescing.
--   Autosave fires on nearly every keystroke, so a row-per-save would explode
--   the table and make the history unreadable ("47 versions in 2 minutes").
--   Instead we coalesce edits into sessions: a save by the SAME author within
--   a short window of the latest version UPDATES that version in place (rolls
--   it forward to the new content); a save by a DIFFERENT author, or after the
--   window has elapsed, INSERTS a new version. The coalescing window is a
--   handler constant, not stored here. This yields a clean, Google-Docs-like
--   timeline: one entry per editing session per person.
--
--   reason distinguishes how a version came to exist:
--     'edit'    — an ordinary (coalesced) save,
--     'manual'  — the user pressed "Save version" (always a fresh row, never
--                 coalesced, optionally carrying a human label),
--     'restore' — written right after a restore so the restore is itself
--                 undoable (the pre-restore state is also captured first).
--
-- version_no is monotonic per note (1,2,3…), assigned by the handler under a
-- per-note guard so the timeline reads naturally. note_id FKs
-- cerebro_note(artifact_id) ON DELETE CASCADE.
CREATE TABLE IF NOT EXISTS cerebro_note_version (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id       UUID NOT NULL REFERENCES cerebro_note(artifact_id) ON DELETE CASCADE,
    version_no    INTEGER NOT NULL,

    title         TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    byte_size     INTEGER NOT NULL DEFAULT 0,

    reason        TEXT NOT NULL DEFAULT 'edit'
                  CHECK (reason IN ('edit', 'manual', 'restore')),
    label         TEXT,

    author_type   TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id     UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (note_id, version_no)
);

-- History render: newest version first for a note.
CREATE INDEX IF NOT EXISTS idx_cerebro_note_version_note
    ON cerebro_note_version (note_id, version_no DESC);
