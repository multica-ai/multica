-- The caller's request key for a run that is not metered against the autopilot
-- run quota, such as a member's own "run now". Metered runs keep theirs on
-- autopilot_quota_reservation; this column lets an unmetered retry reuse its run
-- without touching the quota tables.
ALTER TABLE autopilot_run ADD COLUMN IF NOT EXISTS idempotency_key TEXT;
