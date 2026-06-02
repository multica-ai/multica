ALTER TABLE multica_workflow DROP COLUMN IF EXISTS source_template_id;
ALTER TABLE multica_workflow DROP COLUMN IF EXISTS is_template;
ALTER TABLE multica_user DROP COLUMN IF EXISTS can_manage_workflows;
