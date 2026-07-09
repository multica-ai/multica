ALTER TABLE "user"
  DROP COLUMN IF EXISTS cloud_waitlist_reason,
  DROP COLUMN IF EXISTS cloud_waitlist_email;
