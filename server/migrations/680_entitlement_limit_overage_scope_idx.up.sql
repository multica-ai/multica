-- Upsert key for RecordEntitlementLimitExceeded and the per-workspace lookup.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_entitlement_limit_overage_scope
    ON entitlement_limit_overage (workspace_id, gate, period_start, period_end);
