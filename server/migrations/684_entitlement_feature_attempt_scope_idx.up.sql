-- Upsert key for RecordEntitlementFeatureAttempt and the per-workspace lookup.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_entitlement_feature_attempt_scope
    ON entitlement_feature_attempt (workspace_id, feature, day);
