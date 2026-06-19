-- 125_workflow_stage.up.sql
-- Add workflow_stage table and stage_id column to workflow_node.
--
-- Stages group workflow nodes into logical phases (e.g. "Requirements",
-- "Design", "Review") displayed as columns in the stage overview UI.

CREATE TABLE multica_workflow_stage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES multica_workflow(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_stage_workflow_id ON multica_workflow_stage(workflow_id);
CREATE INDEX idx_workflow_stage_sort_order ON multica_workflow_stage(workflow_id, sort_order);

-- Add stage_id to workflow_node
ALTER TABLE multica_workflow_node
ADD COLUMN stage_id UUID REFERENCES multica_workflow_stage(id) ON DELETE SET NULL;

CREATE INDEX idx_workflow_node_stage_id ON multica_workflow_node(stage_id);
