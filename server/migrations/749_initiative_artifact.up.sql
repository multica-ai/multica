-- The artifact checklist of one stage of one initiative (spec 3.1). The row is
-- keyed by (initiative_id, stage_key, kind), which the unique index in 750
-- enforces, so the table has no surrogate id and no primary key of its own.
-- waived_by marks a waiver; a document satisfies the row by setting
-- knowledge_link_id. No foreign keys.
CREATE TABLE IF NOT EXISTS initiative_artifact (
    workspace_id UUID NOT NULL,
    initiative_id UUID NOT NULL,
    stage_key TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (char_length(kind) BETWEEN 1 AND 64),
    required BOOLEAN NOT NULL DEFAULT true,
    knowledge_link_id UUID,
    waived_by UUID,
    waived_note TEXT NOT NULL DEFAULT '' CHECK (char_length(waived_note) <= 4000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
