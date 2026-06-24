-- Workspace-scoped named secrets encrypted at rest with application-layer secretbox.
-- secret_ref values in workspace settings (e.g. "secret://workspace/curator-key")
-- resolve to rows in this table at runtime.
CREATE TABLE workspace_secret (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    encrypted_value BYTEA NOT NULL,
    created_by UUID NOT NULL REFERENCES member(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name)
);

CREATE INDEX workspace_secret_workspace_id_idx ON workspace_secret(workspace_id);
