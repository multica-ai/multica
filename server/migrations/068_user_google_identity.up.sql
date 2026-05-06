ALTER TABLE "user" ADD COLUMN google_id TEXT;
CREATE UNIQUE INDEX user_google_id_unique ON "user"(google_id) WHERE google_id IS NOT NULL;
ALTER TABLE "user" ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT FALSE;

-- Existing magic-link users completed the verification code flow,
-- so their email is already proven.
UPDATE "user" SET email_verified = TRUE;
