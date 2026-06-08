-- 116_template_system.up.sql
-- Dynamic workflow templates: mark workflows as templates, track lineage,
-- and add workflow admin permission bit to users (global, not workspace-scoped).
ALTER TABLE multica_workflow ADD COLUMN IF NOT EXISTS is_template BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE multica_workflow ADD COLUMN IF NOT EXISTS source_template_id UUID REFERENCES multica_workflow(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_workflow_is_template ON multica_workflow(workspace_id, is_template);
CREATE INDEX IF NOT EXISTS idx_workflow_source_template ON multica_workflow(source_template_id);

ALTER TABLE multica_user ADD COLUMN IF NOT EXISTS can_manage_workflows BOOLEAN NOT NULL DEFAULT FALSE;
