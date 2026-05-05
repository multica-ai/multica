-- CEREBRO-PATCH(migration-idempotent-060-runtime-sandbox-enabled): cerebro modification of upstream file
ALTER TABLE agent_runtime
    DROP COLUMN sandbox_enabled;
