CREATE TABLE tag_access_session (
    tag_session_id TEXT NOT NULL,
    vibes_session_id TEXT NOT NULL,
    vibes_user_id TEXT NOT NULL,
    account_epoch BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_session_account_epoch_check CHECK (account_epoch > 0)
);
