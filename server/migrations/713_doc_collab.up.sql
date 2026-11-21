-- Live co-editing of docs: the Yjs state behind a doc's stored content, the
-- content of recent revisions (so a write based on an older revision can be
-- merged instead of refused), and everyone whose edits a snapshot carries.
-- Primary keys are built concurrently in 714–717.
CREATE TABLE IF NOT EXISTS doc_collab_state (
    doc_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    ystate BYTEA NOT NULL,
    revision BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS doc_revision (
    doc_id UUID NOT NULL,
    revision BIGINT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE doc_version ADD COLUMN IF NOT EXISTS contributors JSONB NOT NULL DEFAULT '[]'::jsonb;
