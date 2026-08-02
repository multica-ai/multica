DROP TABLE IF EXISTS cerebro_company_brain_connection CASCADE;

ALTER TABLE workspace_connection
    DROP CONSTRAINT IF EXISTS workspace_connection_workspace_id_id_unique;
