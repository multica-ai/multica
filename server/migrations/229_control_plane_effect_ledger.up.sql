-- control_plane_effect_ledger is the at-most-once ledger for ORCHESTRATOR-tier
-- control-plane effects under provider failover (td-836aa9). An orchestrator run
-- that is handed off mid-flight is re-planned from scratch by the replacement
-- runtime, so without a guard it would re-dispatch the control-plane effects the
-- primary already made — re-spawning the same child tasks/issues and re-promoting
-- the same stages (a double-dispatch). Before applying such an effect the caller
-- claims its deterministic idempotency key (providerfailover.EffectKey); a second
-- claim of the same (chain, effect, target) is rejected by the UNIQUE constraint,
-- so the effect happens once across the original run AND any fallback.
--
-- No foreign keys / cascades, by repository rule: workspace_id and
-- chain_root_task_id reference other rows but are resolved and cleaned up in
-- application code. This is an append-only audit + dedup trail; stale references
-- are tolerated by readers.
CREATE TABLE IF NOT EXISTS control_plane_effect_ledger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    -- The failover ownership unit this effect belongs to (chat_input_task_id for
    -- chat chains, else the origin task id). Both the original run and a
    -- handed-off fallback share this root, which is why they compute the same
    -- effect_key and contend for the same claim.
    chain_root_task_id UUID NOT NULL,
    -- Kind of control-plane effect. Keep this CHECK in lockstep with the
    -- providerfailover.ControlPlaneEffect constants.
    effect_type TEXT NOT NULL CHECK (effect_type IN (
        'task_spawn',
        'stage_promotion'
    )),
    -- Deterministic idempotency key (providerfailover.EffectKey) — a hash of
    -- (chain_root, effect_type, target). UNIQUE is the at-most-once enforcement.
    effect_key TEXT NOT NULL UNIQUE,
    -- Human-readable target identity for audit (e.g. the spawned child id, or
    -- "<parent_issue_id>:stage=<n>"). Not used for dedup — effect_key is.
    target_ref TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Reverse lookup: every control-plane effect recorded for a chain, for the
-- observability API and for reconciling what an interrupted orchestrator did.
CREATE INDEX IF NOT EXISTS control_plane_effect_ledger_chain_idx
    ON control_plane_effect_ledger (chain_root_task_id);
