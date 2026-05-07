-- CEREBRO-PATCH(migration-idempotent-060-runtime-sandbox-enabled): cerebro modification of upstream file
-- Per-runtime sandbox override (JEH-418). NULL means "inherit the daemon's
-- MULTICA_ENABLE_SANDBOX value" so existing rows keep current behaviour;
-- TRUE/FALSE forces sandbox on/off for tasks claimed by this runtime,
-- regardless of what the daemon's env var says.
ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS sandbox_enabled BOOLEAN;
