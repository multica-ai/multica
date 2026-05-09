-- Reverses 079_ship_hub.up.sql.
ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_enabled;

DROP INDEX IF EXISTS idx_deploy_environment_status;
DROP TABLE IF EXISTS deploy;

DROP INDEX IF EXISTS idx_deploy_environment_workspace;
DROP TABLE IF EXISTS deploy_environment;

DROP INDEX IF EXISTS idx_pull_request_project;
DROP INDEX IF EXISTS idx_pull_request_workspace_state;
DROP TABLE IF EXISTS pull_request;

DROP TYPE IF EXISTS deploy_status;
DROP TYPE IF EXISTS deploy_environment_kind;
DROP TYPE IF EXISTS pull_request_state;
