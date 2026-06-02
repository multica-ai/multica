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
    override_content TEXT NOT NULL
);

-- Use COALESCE with sentinel UUID to enforce uniqueness with nullable columns.
-- PostgreSQL treats NULLs as distinct in UNIQUE constraints, so two rows with
-- (template_id, NULL, NULL) would not violate a plain UNIQUE. The expression
-- index maps NULL → sentinel before comparing.
CREATE UNIQUE INDEX workflow_prompt_overrides_template_agent_project_key
    ON workflow_prompt_overrides (
        template_id,
        COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid)
    );

CREATE INDEX idx_workflow_prompt_overrides_template
    ON workflow_prompt_overrides (template_id);

-- Seed default prompt templates for each stage.
-- These are workspace-less system defaults seeded once at migration time.
-- Application code should copy these into new workspaces on workspace creation.
INSERT INTO workflow_prompt_templates (workspace_id, stage, name, content, is_default)
SELECT w.id, s.stage, 'default',
    CASE s.stage
        WHEN 'plan'    THEN 'Analyze the task and create a step-by-step execution plan. Break down the work into discrete, testable units. Identify risks and dependencies.'
        WHEN 'execute' THEN 'Implement the plan step by step. Write clean, well-tested code. Follow project conventions. Commit frequently with clear messages.'
        WHEN 'review'  THEN 'Review the implementation for correctness, security, and performance. Check edge cases. Verify all acceptance criteria are met.'
        WHEN 'deploy'  THEN 'Prepare for deployment. Run all tests, lint checks, and type checks. Verify CI passes. Update documentation if needed.'
    END,
    true
FROM workspace w
CROSS JOIN (VALUES ('plan'), ('execute'), ('review'), ('deploy')) AS s(stage);
