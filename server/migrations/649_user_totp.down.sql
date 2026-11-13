ALTER TABLE "user" DROP COLUMN IF EXISTS totp_locked_until;
ALTER TABLE "user" DROP COLUMN IF EXISTS totp_failed_attempts;
ALTER TABLE "user" DROP COLUMN IF EXISTS totp_last_used_step;
ALTER TABLE "user" DROP COLUMN IF EXISTS totp_enabled_at;
ALTER TABLE "user" DROP COLUMN IF EXISTS totp_secret_encrypted;
