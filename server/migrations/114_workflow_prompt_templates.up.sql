-- Workflow prompt templates: editable daemon workflow prompts per workspace.
-- Part of S5-F7 (AIH-56).

CREATE TABLE workflow_prompt_templates (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    stage        VARCHAR(50) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    content      TEXT NOT NULL,
    is_default   BOOLEAN DEFAULT false,
    version      INTEGER DEFAULT 1,
    created_at   TIMESTAMPTZ DEFAULT now(),
    updated_at   TIMESTAMPTZ DEFAULT now(),
    CONSTRAINT workflow_prompt_templates_stage_check
        CHECK (stage IN ('plan', 'execute', 'review', 'deploy')),
    CONSTRAINT workflow_prompt_templates_workspace_stage_name_key
        UNIQUE (workspace_id, stage, name)
);

CREATE INDEX idx_workflow_prompt_templates_workspace
    ON workflow_prompt_templates (workspace_id);

CREATE TABLE workflow_prompt_overrides (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id     UUID NOT NULL REFERENCES workflow_prompt_templates(id) ON DELETE CASCADE,
    agent_id        UUID REFERENCES agent(id) ON DELETE CASCADE,
    project_id      UUID REFERENCES project(id) ON DELETE CASCADE,
    override_content TEXT NOT NULL,
    CONSTRAINT workflow_prompt_overrides_template_agent_project_key
        UNIQUE (template_id, agent_id, project_id)
);

CREATE INDEX idx_workflow_prompt_overrides_template
    ON workflow_prompt_overrides (template_id);
