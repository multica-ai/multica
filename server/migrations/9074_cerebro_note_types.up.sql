-- TECH-3511: Note types — reusable note templates with recurrence, built on
-- top of the artifact feature. A note type carries a fixed markdown template
-- plus a recurrence behaviour ('running_doc' = one rolling document with the
-- newest section prepended; 'new_note' = a fresh note per period in a folder).
-- The daily note-types sweeper materialises the template on cadence; a manual
-- "run now" endpoint covers off-cycle reviews.

CREATE TABLE IF NOT EXISTS cerebro_note_type (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id            UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    icon                    TEXT NOT NULL DEFAULT '',
    template_body           TEXT NOT NULL DEFAULT '',
    recurrence_mode         TEXT NOT NULL DEFAULT 'running_doc'
                                CHECK (recurrence_mode IN ('running_doc', 'new_note')),
    cadence_unit            TEXT NOT NULL DEFAULT 'manual'
                                CHECK (cadence_unit IN ('manual', 'week', 'month', 'quarter')),
    cadence_count           INT NOT NULL DEFAULT 1 CHECK (cadence_count > 0),
    target_folder_id        UUID REFERENCES artifact_folder(id) ON DELETE SET NULL,
    running_doc_artifact_id UUID REFERENCES artifact(id) ON DELETE SET NULL,
    last_period_key         TEXT NOT NULL DEFAULT '',
    enabled                 BOOLEAN NOT NULL DEFAULT TRUE,
    created_by              UUID NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_note_type_workspace ON cerebro_note_type(workspace_id);
-- Partial index the daily sweeper scans: only enabled, scheduled (non-manual) types.
CREATE INDEX IF NOT EXISTS idx_cerebro_note_type_sweep
    ON cerebro_note_type(enabled) WHERE enabled AND cadence_unit <> 'manual';

-- Link materialised notes back to their type + the period they represent.
ALTER TABLE artifact ADD COLUMN IF NOT EXISTS note_type_id UUID
    REFERENCES cerebro_note_type(id) ON DELETE SET NULL;
ALTER TABLE artifact ADD COLUMN IF NOT EXISTS period_key TEXT;

-- Idempotency for 'new_note' mode: never create two notes for the same
-- (note type, period). 'running_doc' notes leave period_key NULL (one row
-- is reused across periods) and are therefore excluded from this constraint.
CREATE UNIQUE INDEX IF NOT EXISTS uq_artifact_note_type_period
    ON artifact(note_type_id, period_key)
    WHERE note_type_id IS NOT NULL AND period_key IS NOT NULL;
