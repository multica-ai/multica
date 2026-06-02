ALTER TABLE workflow DROP COLUMN IF EXISTS source_template_id;
ALTER TABLE workflow DROP COLUMN IF EXISTS is_template;
ALTER TABLE "user" DROP COLUMN IF EXISTS can_manage_workflows;
