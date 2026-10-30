-- Dropping the constraint in migration 359 also drops the attached index, so
-- this is normally a safe no-op.
DROP INDEX CONCURRENTLY IF EXISTS autopilot_quota_reservation_pkey_uidx;
