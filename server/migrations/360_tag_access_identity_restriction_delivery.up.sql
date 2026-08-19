-- Per-user access-revocation delivery evidence. This table is not VIBES
-- identity/account authority and cannot create or update business identities.
CREATE TABLE tag_access_identity_restriction_delivery (
    vibes_user_id TEXT NOT NULL,
    identity_restriction_version BIGINT NOT NULL,
    restriction_kind TEXT NOT NULL,
    vibes_session_id TEXT,
    account_epoch BIGINT NOT NULL,
    event_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_digest BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_identity_delivery_version_check CHECK (identity_restriction_version > 0),
    CONSTRAINT tag_access_identity_delivery_kind_check CHECK (restriction_kind IN ('session_logged_out', 'account_banned')),
    CONSTRAINT tag_access_identity_delivery_session_check CHECK (
        (restriction_kind = 'session_logged_out' AND vibes_session_id IS NOT NULL) OR
        (restriction_kind = 'account_banned' AND vibes_session_id IS NULL)
    ),
    CONSTRAINT tag_access_identity_delivery_epoch_check CHECK (account_epoch > 0),
    CONSTRAINT tag_access_identity_delivery_digest_check CHECK (octet_length(payload_digest) = 32)
);
