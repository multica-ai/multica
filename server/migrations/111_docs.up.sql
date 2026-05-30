CREATE TABLE doc (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    creator_id   UUID NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    parent_id    UUID REFERENCES doc(id) ON DELETE SET NULL,
    position     FLOAT8 NOT NULL DEFAULT 0,
    is_archived  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX doc_workspace_id_idx ON doc(workspace_id) WHERE is_archived = FALSE;
CREATE INDEX doc_parent_id_idx ON doc(parent_id) WHERE parent_id IS NOT NULL;
