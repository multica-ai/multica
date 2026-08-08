-- Autopilot chain / cross-autopilot orchestration triggers (WS-768 / S4).
--
-- A chain trigger lives on the DOWNSTREAM autopilot (same shape as schedule /
-- webhook: the trigger declares "who gets fired"). Its `upstream_autopilot_id`
-- names the upstream autopilot whose run reaching a terminal state fans out a
-- downstream run. `chain_on_status` filters which terminal status of the
-- upstream run fires the edge: 'completed' (success chain), 'failed' (error
-- handler chain), or 'any'.
--
-- Reuses autopilot_trigger rather than a separate relationship table: every
-- other dispatch source is already a trigger kind, so a chain edge is just
-- one more kind on the downstream. This keeps trigger CRUD / publisher /
-- enable-disable / permissions uniform, and lets a downstream compose its own
-- chain triggers to form a DAG naturally (the rejected alternative - an
-- autopilot-level successor_autopilot_ids[] array - could not FK, could not be
-- enabled/disabled per-edge, and could not carry per-edge status filters).
--
-- The downstream run records `chain_depth` (backstop cycle guard) and
-- `chain_upstream_run_id` (idempotency anchor). The partial unique index on
-- (chain_upstream_run_id, trigger_id) is added in migration 215 so it can be
-- built CONCURRENTLY in its own single-statement file (per repo migration rule).

-- Extend the trigger kind CHECK to admit 'chain'.
ALTER TABLE autopilot_trigger DROP CONSTRAINT autopilot_trigger_kind_check;
ALTER TABLE autopilot_trigger ADD CONSTRAINT autopilot_trigger_kind_check
    CHECK (kind IN ('schedule', 'webhook', 'api', 'chain'));

-- Chain edge config columns.
ALTER TABLE autopilot_trigger
    ADD COLUMN upstream_autopilot_id UUID REFERENCES autopilot(id) ON DELETE CASCADE,
    ADD COLUMN chain_on_status TEXT NOT NULL DEFAULT 'completed'
        CHECK (chain_on_status IN ('completed', 'failed', 'any'));

-- A chain trigger must name its upstream; non-chain triggers must not.
ALTER TABLE autopilot_trigger ADD CONSTRAINT autopilot_trigger_chain_upstream_check
    CHECK (kind <> 'chain' OR upstream_autopilot_id IS NOT NULL);

-- Extend the run source CHECK to admit 'chain' (downstream runs fired by an
-- upstream terminal transition).
ALTER TABLE autopilot_run DROP CONSTRAINT autopilot_run_source_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_source_check
    CHECK (source IN ('schedule', 'manual', 'webhook', 'api', 'chain'));

-- Chain depth (0 for non-chain runs; upstream_depth + 1 for a chain run) and
-- the upstream run that fired this run. chain_upstream_run_id is the
-- idempotency anchor: at most one downstream run per (upstream run, chain
-- trigger), enforced by migration 215's partial unique index.
ALTER TABLE autopilot_run
    ADD COLUMN chain_depth INT NOT NULL DEFAULT 0,
    ADD COLUMN chain_upstream_run_id UUID REFERENCES autopilot_run(id) ON DELETE SET NULL;
