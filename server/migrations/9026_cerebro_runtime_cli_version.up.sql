-- W4.2: persist the daemon's CLI version on the runtime row so we can
-- detect drift on heartbeat. When the value changes between heartbeats,
-- the runtime's tool surface may have changed and persona's
-- scanner-discovery view is now stale — the heartbeat handler refreshes
-- the capabilities JSONB if the daemon included one in the same payload.
ALTER TABLE agent_runtime ADD COLUMN IF NOT EXISTS cli_version TEXT;
