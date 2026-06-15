-- Cerebro: default the interactive terminal ON for runtimes that support it.
-- Daemon-backed (local) runtimes can stream their agent output to the broker;
-- cloud providers (firtal-gateway) execute server-side and have no local
-- stream to tee, so they stay headless. This backfills the existing fleet;
-- newly-registered daemon runtimes are set to 'interactive' at registration
-- (server/internal/handler/daemon.go, CEREBRO-PATCH(runtime-default-interactive)).
-- Only rows still on the original 'headless' default are flipped, so an
-- explicit headless choice made before this migration is preserved.

UPDATE agent_runtime
SET presentation_mode = 'interactive', updated_at = now()
WHERE presentation_mode = 'headless'
  AND provider <> 'firtal-gateway';
