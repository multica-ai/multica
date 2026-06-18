CREATE TABLE channel_group (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_group_workspace ON channel_group(workspace_id, position);

ALTER TABLE channel ADD COLUMN group_id UUID REFERENCES channel_group(id) ON DELETE SET NULL;
ALTER TABLE channel ADD COLUMN position DOUBLE PRECISION NOT NULL DEFAULT 0;
