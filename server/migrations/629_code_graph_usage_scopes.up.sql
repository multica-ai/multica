ALTER TABLE code_graph_repository ADD COLUMN IF NOT EXISTS is_workspace_shared BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS project_code_graph_repository (
    project_id UUID NOT NULL,
    repository_id UUID NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
