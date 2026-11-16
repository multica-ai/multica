-- Dropping the constraint in migration 359 also drops the attached index, so
-- this is normally a safe no-op.
DROP INDEX CONCURRENTLY IF EXISTS uq_autopilot_quota_period_scope;
