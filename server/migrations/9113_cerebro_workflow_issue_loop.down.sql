DROP INDEX IF EXISTS idx_cerebro_workflow_workflow_type;
DROP INDEX IF EXISTS idx_cerebro_workflow_generated_from;

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_workflow_type_known;

ALTER TABLE cerebro_workflow
    DROP COLUMN IF EXISTS generated_from_workflow_id,
    DROP COLUMN IF EXISTS loop_spec,
    DROP COLUMN IF EXISTS workflow_type;
