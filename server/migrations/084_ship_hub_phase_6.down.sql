-- Reverse Phase 6 schema. Drop the adapter-config table first (it's a
-- child of deploy_environment via FK), then strip the column.

DROP INDEX IF EXISTS idx_deploy_adapter_config_kind;
DROP TABLE IF EXISTS deploy_adapter_config;
ALTER TABLE deploy_environment DROP COLUMN IF EXISTS adapter_kind;
