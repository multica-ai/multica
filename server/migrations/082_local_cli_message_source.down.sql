DROP INDEX IF EXISTS idx_local_cli_message_run_source_key;

ALTER TABLE local_cli_message
    DROP COLUMN IF EXISTS source_key,
    DROP COLUMN IF EXISTS source;
