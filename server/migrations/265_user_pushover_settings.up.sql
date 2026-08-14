ALTER TABLE "user"
    ADD COLUMN pushover_user_key TEXT,
    ADD COLUMN pushover_login_codes_enabled BOOLEAN NOT NULL DEFAULT FALSE;
