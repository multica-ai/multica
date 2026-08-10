-- NEX-38: widen agent_runtime.status so a daemon in safe-shutdown (drain)
-- mode is a first-class state instead of being flattened to 'online'.
--
-- Semantics (see the NEX-38 drain design):
--   - 'online':  can claim work (unchanged).
--   - 'draining': no longer receives new triggers (AgentReadiness rejects),
--     but keeps heartbeating last_seen_at so the offline sweeper does not
--     misjudge an in-flight drain as a dead runtime.
--   - 'offline': unchanged.
--
-- Drop-and-re-add of the CHECK, matching the constraint name Postgres
-- auto-generated from migration 004 (agent_runtime_status_check). No new
-- columns, no FKs/cascades, no indexes — this is purely a constraint
-- widening and does not need a concurrent-index file.
ALTER TABLE agent_runtime DROP CONSTRAINT IF EXISTS agent_runtime_status_check;
ALTER TABLE agent_runtime ADD CONSTRAINT agent_runtime_status_check
    CHECK (status IN ('online', 'draining', 'offline'));
