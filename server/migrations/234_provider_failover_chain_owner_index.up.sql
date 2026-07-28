-- At-most-one active fallback per task chain: partial to the OWNING states so
-- shadow/declined/failed/superseded rows never block a later legitimate handoff.
-- This is the atomic backstop for the max-one-per-chain policy under concurrency.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS provider_failover_chain_owner_uidx ON provider_failover_handoff (chain_root_task_id) WHERE state IN ('HANDOFF_PENDING', 'HANDOFF_DISPATCHED', 'HANDOFF_COMPLETED');
