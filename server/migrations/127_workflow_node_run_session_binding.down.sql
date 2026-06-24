DROP INDEX IF EXISTS idx_workflow_node_run_session_id;
ALTER TABLE multica_workflow_node_run
    DROP COLUMN IF EXISTS session_id,
    DROP COLUMN IF EXISTS device_id,
    DROP COLUMN IF EXISTS runtime_id;
