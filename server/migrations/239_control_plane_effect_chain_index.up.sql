-- Reverse lookup: every control-plane effect recorded for a chain, for the
-- observability API and for reconciling what an interrupted orchestrator did.
--
-- Repository migration policy requires production indexes to be created
-- concurrently and isolated in their own migration so table writes are not
-- blocked while the index is built.
CREATE INDEX CONCURRENTLY IF NOT EXISTS control_plane_effect_ledger_chain_idx
    ON control_plane_effect_ledger (chain_root_task_id);
