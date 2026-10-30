ALTER TABLE issue_wakeup
 DROP COLUMN IF EXISTS timed_out_at,
 DROP COLUMN IF EXISTS on_timeout,
 DROP COLUMN IF EXISTS expiry_seconds,
 DROP COLUMN IF EXISTS expires_at;
