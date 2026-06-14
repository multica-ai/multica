-- Notes feature (TECH-3421): a lightweight, fast, private-by-default note that
-- is built ON TOP of the existing artifact (Document) machinery. A "Note" is an
-- artifact with kind='note' PLUS a cerebro_note row that carries the
-- note-specific state the upstream artifact model does not have: an owner,
-- a visibility level, and pin state. Keeping this in a cerebro side-table means
-- the upstream `artifact` table is untouched (fork-clean), while notes still
-- reuse the artifact editor, storage, folders and search.
--
--   owner_id    — the member who created the note. Drives "private = only me".
--   visibility  — who may see the note:
--                   'private'   = only the owner (default),
--                   'shared'    = owner + the users listed in cerebro_note_share,
--                   'workspace' = every member of the workspace.
--   pinned      — surfaces the note at the top of the fast list. pinned_at gives
--                 a stable sort among pinned notes.
--
-- Absence of a cerebro_note row for a kind='note' artifact means it is a plain
-- agent/CLI-authored note document, not a Notes-feature note; those keep their
-- existing scope-based visibility and never appear in the personal Notes list.
CREATE TABLE IF NOT EXISTS cerebro_note (
    artifact_id uuid        PRIMARY KEY,
    owner_id    uuid        NOT NULL,
    visibility  text        NOT NULL DEFAULT 'private'
                CHECK (visibility IN ('private', 'shared', 'workspace')),
    pinned      boolean     NOT NULL DEFAULT false,
    pinned_at   timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cerebro_note_owner_idx
    ON cerebro_note (owner_id);

-- Explicit per-user shares, only meaningful when visibility = 'shared'.
-- Mirrors the "share with specific colleagues" requirement.
CREATE TABLE IF NOT EXISTS cerebro_note_share (
    artifact_id uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (artifact_id, user_id)
);

CREATE INDEX IF NOT EXISTS cerebro_note_share_user_idx
    ON cerebro_note_share (user_id);
