ALTER TABLE instructions_history DROP CONSTRAINT IF EXISTS fk_instructions_history_template;
ALTER TABLE instructions_history DROP COLUMN IF EXISTS template_id;
