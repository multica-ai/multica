-- Adds per-user TOTP state. NULL totp_secret_encrypted = not set up.
-- totp_secret_encrypted IS NOT NULL but enabled_at IS NULL = setup in
-- progress (secret generated but first verification not completed).
-- Both non-NULL = TOTP enabled for login.
ALTER TABLE "user" ADD COLUMN totp_secret_encrypted BYTEA;
ALTER TABLE "user" ADD COLUMN totp_enabled_at       TIMESTAMPTZ;
