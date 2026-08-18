CREATE TABLE vibes_workspace_mirror (
    vibes_workspace_id TEXT NOT NULL,
    multica_workspace_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
