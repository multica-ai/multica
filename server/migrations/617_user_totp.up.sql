-- Adds per-user TOTP state. NULL totp_secret_encrypted = not set up.
-- totp_secret_encrypted IS NOT NULL but enabled_at IS NULL = setup in
-- progress (secret generated but first verification not completed).
-- Both non-NULL = TOTP enabled for login.
--
-- totp_last_used_step is the RFC 6238 time step of the last accepted code;
-- a code is only accepted for a later step, so an observed code cannot be
-- replayed. totp_failed_attempts / totp_locked_until form an account-level
-- attempt budget that works without Redis-backed rate limiting.
--
-- IF NOT EXISTS keeps this safe for self-hosted instances that already ran
-- an earlier revision of this change under a different migration number.
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS totp_secret_encrypted BYTEA;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS totp_enabled_at       TIMESTAMPTZ;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS totp_last_used_step   BIGINT;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS totp_failed_attempts  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS totp_locked_until     TIMESTAMPTZ;
