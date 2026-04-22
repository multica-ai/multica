-- Runtime identity is moving from `os.Hostname()` to a persistent daemon UUID.
-- `legacy_daemon_id` records the most recent hostname-derived daemon_id that
-- was merged into this row so the previous identity remains traceable for
-- debugging and audit after the old row is deleted.
ALTER TABLE agent_runtime
    ADD COLUMN legacy_daemon_id TEXT;
