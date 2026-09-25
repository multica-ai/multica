ALTER TABLE "user" DROP COLUMN IF EXISTS totp_enabled_at;
ALTER TABLE "user" DROP COLUMN IF EXISTS totp_secret_encrypted;
