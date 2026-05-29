-- Backfill the Firtal Gateway server runtime to 'public' visibility so any
-- workspace member can see it on the runtimes page and bind agents to it.
-- The runtime is auto-registered by the server-side FirtalGatewayExecutor
-- via UpsertAgentRuntime, which doesn't set visibility, so existing rows
-- inherited the agent_runtime default of 'private' (migration 083). The
-- executor was patched to flip freshly-inserted rows to 'public', but the
-- upsert's ON CONFLICT branch deliberately doesn't touch visibility (to
-- preserve any later manual toggle), so previously-registered rows never
-- pick the new default up. This one-shot UPDATE catches them.
UPDATE agent_runtime
SET visibility = 'public', updated_at = now()
WHERE daemon_id = 'server:firtal-gateway'
  AND visibility = 'private';
