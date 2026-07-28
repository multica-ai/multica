-- One ledger row per failed task: makes the record write idempotent (the
-- evaluation runs on the FailTask post-commit path, which a duplicated terminal
-- callback can re-enter). Callers insert ON CONFLICT DO NOTHING.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS provider_failover_original_task_uidx ON provider_failover_handoff (original_task_id);
