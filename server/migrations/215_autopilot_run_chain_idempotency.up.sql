-- Chain dispatch idempotency: at most one downstream run per
-- (upstream run, chain trigger). When an upstream run reaches a terminal
-- state and the terminal transition fires twice (event replay, worker
-- resume, or a SyncRunFrom* listener racing itself), the partial unique
-- index turns the second CreateAutopilotRun into a 23505 conflict the
-- caller recovers into "reuse the existing run" - mirroring the webhook
-- delivery admission path (migration 177) and the scheduled
-- (trigger_id, planned_at) guard (migration 124).
--
-- Partial because only source='chain' runs carry the anchor; every other
-- source leaves chain_upstream_run_id NULL and must not participate.
--
-- Single-statement file + CONCURRENTLY per the repo migration rule (index
-- builds cannot share a transaction / multi-command string with the
-- ALTERs in 214).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_autopilot_run_chain_upstream
    ON autopilot_run(chain_upstream_run_id, trigger_id)
    WHERE source = 'chain' AND chain_upstream_run_id IS NOT NULL;
