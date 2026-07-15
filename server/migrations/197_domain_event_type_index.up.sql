-- Single-statement CONCURRENTLY migration (see 194). Backs per-workspace,
-- per-type event scans used by explain/debug tooling:
--   WHERE workspace_id = $1 AND type = $2 ORDER BY seq
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_domain_event_type
    ON domain_event (workspace_id, type, seq);
