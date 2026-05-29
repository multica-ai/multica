DROP INDEX IF EXISTS idx_local_cli_run_workspace_source_key;

ALTER TABLE local_cli_run
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS source_key;
