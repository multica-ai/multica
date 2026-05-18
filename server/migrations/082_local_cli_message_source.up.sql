ALTER TABLE local_cli_message
    ADD COLUMN source TEXT,
    ADD COLUMN source_key TEXT;

CREATE UNIQUE INDEX idx_local_cli_message_run_source_key
    ON local_cli_message(run_id, source, source_key)
    WHERE source IS NOT NULL AND source_key IS NOT NULL;
