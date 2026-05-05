-- CEREBRO-PATCH(migration-idempotent-048-runtime-daemon-uuid): cerebro modification of upstream file
-- Runtime identity is moving from `os.Hostname()` to a persistent daemon UUID.
-- `legacy_daemon_id` records the most recent hostname-derived daemon_id that
-- was merged into this row so the previous identity remains traceable for
-- debugging and audit after the old row is deleted.
ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS legacy_daemon_id TEXT;
