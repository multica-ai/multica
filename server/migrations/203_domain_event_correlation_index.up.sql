-- Single-statement CONCURRENTLY migration (see 201). Backs correlation-chain
-- reads (GET /api/events?correlation_id=) and loop/depth guardrail lookups:
--   WHERE workspace_id = $1 AND correlation_id = $2 ORDER BY seq
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_domain_event_correlation
    ON domain_event (workspace_id, correlation_id, seq);
