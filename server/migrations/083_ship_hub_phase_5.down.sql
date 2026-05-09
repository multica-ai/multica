-- Reverse 083_ship_hub_phase_5.up.sql in dependency order.

DROP INDEX IF EXISTS idx_deploy_health_workspace;
DROP INDEX IF EXISTS idx_deploy_health_deploy;
DROP TABLE IF EXISTS deploy_health_snapshot;

DROP INDEX IF EXISTS idx_deploy_preflight_env;
DROP INDEX IF EXISTS idx_deploy_preflight_workspace;
DROP TABLE IF EXISTS deploy_preflight;

DROP INDEX IF EXISTS idx_pull_request_risk;
ALTER TABLE pull_request
    DROP COLUMN IF EXISTS risk_classified_at,
    DROP COLUMN IF EXISTS risk_reasons,
    DROP COLUMN IF EXISTS risk_level;
DROP TYPE IF EXISTS risk_level;
