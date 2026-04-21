CREATE TABLE pipeline (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_pipeline_workspace_name_active ON pipeline(workspace_id, name) WHERE deleted_at IS NULL;
CREATE INDEX idx_pipeline_workspace_active ON pipeline(workspace_id) WHERE deleted_at IS NULL;

CREATE TABLE pipeline_column (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id UUID NOT NULL REFERENCES pipeline(id) ON DELETE CASCADE,
    status_key TEXT NOT NULL,
    label TEXT NOT NULL,
    position INT NOT NULL,
    is_terminal BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_transitions TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pipeline_id, status_key),
    UNIQUE (pipeline_id, position)
);
CREATE INDEX idx_pipeline_column_pipeline ON pipeline_column(pipeline_id);

ALTER TABLE workspace_column_config
    ADD COLUMN pipeline_id UUID REFERENCES pipeline(id) ON DELETE CASCADE;

ALTER TABLE workspace_column_config
    DROP CONSTRAINT workspace_column_config_workspace_id_status_key;

ALTER TABLE workspace_column_config
    ADD CONSTRAINT workspace_column_config_unique
    UNIQUE NULLS NOT DISTINCT (workspace_id, pipeline_id, status);
