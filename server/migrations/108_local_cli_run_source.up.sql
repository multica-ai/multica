ALTER TABLE local_cli_run
    ADD COLUMN source TEXT,
    ADD COLUMN source_key TEXT;

CREATE UNIQUE INDEX idx_local_cli_run_workspace_source_key
    ON local_cli_run(workspace_id, source, source_key)
    WHERE source IS NOT NULL AND source_key IS NOT NULL;
