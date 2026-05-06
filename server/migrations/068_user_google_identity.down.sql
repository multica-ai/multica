DROP INDEX IF EXISTS user_google_id_unique;
ALTER TABLE "user" DROP COLUMN IF EXISTS google_id;
ALTER TABLE "user" DROP COLUMN IF EXISTS email_verified;
