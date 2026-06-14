-- TECH-3421 — Backend fundament for cerebro_note_reference. Mirrors
-- cerebro_issue_reference (migration 9017) but keyed by note_id (the note's
-- artifact id) instead of issue_id. Stores arbitrary typed references attached
-- to a Note (e.g. a linked issue now, business objects later). Generic shape on
-- purpose so the frontend can grow new object kinds without a migration per
-- kind: `(object, type)` describes what the row points at, `ref_id` is the
-- external/internal identifier, `metadata` carries payload-specific fields.
--
-- Uniqueness is `(note_id, object, ref_id)` so the same external entity is
-- attached at most once per note — the POST handler relies on this to UPSERT
-- idempotently. note_id FKs cerebro_note(artifact_id) ON DELETE CASCADE so
-- deleting a note drops its references with it (the note row itself is removed
-- before the underlying artifact, so the cascade fires off cerebro_note).

CREATE TABLE IF NOT EXISTS cerebro_note_reference (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id         UUID NOT NULL REFERENCES cerebro_note(artifact_id) ON DELETE CASCADE,
    object          TEXT NOT NULL,
    type            TEXT,
    ref_id          TEXT NOT NULL,
    label           TEXT,
    url             TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id   UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (note_id, object, ref_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_note_reference_note
    ON cerebro_note_reference (note_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_note_reference_lookup
    ON cerebro_note_reference (object, ref_id);
