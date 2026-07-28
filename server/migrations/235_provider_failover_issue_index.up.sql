-- Issue lookup for the observability read API (list handoffs on an issue).
CREATE INDEX CONCURRENTLY IF NOT EXISTS provider_failover_issue_idx ON provider_failover_handoff (issue_id) WHERE issue_id IS NOT NULL;
