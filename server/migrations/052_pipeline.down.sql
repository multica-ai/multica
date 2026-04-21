ALTER TABLE workspace_column_config
    DROP CONSTRAINT workspace_column_config_unique;

ALTER TABLE workspace_column_config
    DROP COLUMN pipeline_id;

ALTER TABLE workspace_column_config
    ADD CONSTRAINT workspace_column_config_workspace_id_status_key
    UNIQUE (workspace_id, status);

DROP INDEX idx_pipeline_column_pipeline;
DROP TABLE pipeline_column;

DROP INDEX idx_pipeline_workspace_name_active;
DROP INDEX idx_pipeline_workspace_active;
DROP TABLE pipeline;
