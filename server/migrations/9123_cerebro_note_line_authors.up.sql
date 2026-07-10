-- FIR-2810: Per-line attribution + author codes for Notes.
--
-- 1. cerebro_note_line_attr — one row per Notes-feature note holding a JSONB
--    array with one entry per body line: who created the line and who last
--    edited it (Apple Notes-style attribution). base_body is the exact body
--    the attrs were computed for, so any read can detect an out-of-band edit
--    (e.g. an agent writing through the artifact API) and self-heal by
--    diffing base_body → current body.
-- 2. author_codes on cerebro_note — per-note toggle: when on, the editor
--    stamps the writer's member code (e.g. "JEH") on every line they write.
-- 3. author_codes on cerebro_note_type — the same toggle on a recurring note
--    (e.g. a business review); materialised notes inherit it.

CREATE TABLE IF NOT EXISTS cerebro_note_line_attr (
    artifact_id uuid        PRIMARY KEY REFERENCES artifact(id) ON DELETE CASCADE,
    base_body   text        NOT NULL DEFAULT '',
    attrs       jsonb       NOT NULL DEFAULT '[]',
    updated_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE cerebro_note
    ADD COLUMN IF NOT EXISTS author_codes boolean NOT NULL DEFAULT false;

ALTER TABLE cerebro_note_type
    ADD COLUMN IF NOT EXISTS author_codes boolean NOT NULL DEFAULT false;
