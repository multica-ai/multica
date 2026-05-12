DROP INDEX IF EXISTS idx_deploy_env_succeeded_triggered;
ALTER TABLE deploy DROP COLUMN IF EXISTS provenance_ref;
ALTER TABLE deploy DROP COLUMN IF EXISTS provenance;
DROP TYPE IF EXISTS deploy_provenance;
