-- 125_workflow_stage.down.sql
-- Rollback: remove stage_id column and drop workflow_stage table.

-- Remove stage_id from workflow_node
DROP INDEX IF EXISTS idx_workflow_node_stage_id;
ALTER TABLE multica_workflow_node DROP COLUMN IF EXISTS stage_id;

-- Drop workflow_stage table
DROP INDEX IF EXISTS idx_workflow_stage_sort_order;
DROP INDEX IF EXISTS idx_workflow_stage_workflow_id;
DROP TABLE IF EXISTS multica_workflow_stage;
