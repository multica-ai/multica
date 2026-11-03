BEGIN;

ALTER TABLE project_engineering
    ADD COLUMN IF NOT EXISTS stages JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE project_engineering DROP CONSTRAINT IF EXISTS project_engineering_stages_check;
ALTER TABLE project_engineering
    ADD CONSTRAINT project_engineering_stages_check
    CHECK (CASE WHEN jsonb_typeof(stages) = 'array'
                THEN jsonb_array_length(stages) <= 50
                ELSE false END);

COMMIT;
