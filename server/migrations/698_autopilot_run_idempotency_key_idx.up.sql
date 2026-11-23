CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_autopilot_run_idempotency_key ON autopilot_run (autopilot_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
