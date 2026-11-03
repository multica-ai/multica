-- Pipelines (spec 3.1). A pipeline is the ordered stages an initiative moves
-- through. stages holds each stage's owner, artifacts, exit and SLA; workflow_id
-- is the issue workflow with one status per stage, and project_id is the board
-- project the initiatives' backing issues live in.
--
-- No foreign keys: workspace, workflow and project are validated in application
-- code. The primary key is built concurrently in 738 and attached in 739.
CREATE TABLE IF NOT EXISTS pipeline (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    workflow_id UUID NOT NULL,
    project_id UUID NOT NULL,
    stages JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(stages) = 'array'),
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
