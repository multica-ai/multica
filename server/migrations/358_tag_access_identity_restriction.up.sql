-- Per-user access-revocation delivery evidence. These tables are not VIBES
-- identity/account authority and cannot create or update business identities.
CREATE TABLE tag_access_identity_restriction_state (
    vibes_user_id TEXT NOT NULL,
    identity_restriction_version BIGINT NOT NULL DEFAULT 0,
    observed_identity_restriction_version BIGINT NOT NULL DEFAULT 0,
    account_epoch BIGINT NOT NULL DEFAULT 0,
    revoked_through_account_epoch BIGINT NOT NULL DEFAULT 0,
    integrity_state TEXT NOT NULL DEFAULT 'gap',
    blocked_payload_digest BYTEA NOT NULL DEFAULT ''::bytea,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_identity_state_version_check CHECK (identity_restriction_version >= 0),
    CONSTRAINT tag_access_identity_state_observed_check CHECK (observed_identity_restriction_version >= identity_restriction_version),
    CONSTRAINT tag_access_identity_state_epoch_check CHECK (account_epoch >= 0),
    CONSTRAINT tag_access_identity_state_revoked_epoch_check CHECK (revoked_through_account_epoch >= 0),
    CONSTRAINT tag_access_identity_state_integrity_check CHECK (integrity_state IN ('healthy', 'gap', 'conflict')),
    CONSTRAINT tag_access_identity_state_digest_check CHECK (octet_length(blocked_payload_digest) IN (0, 32))
);
