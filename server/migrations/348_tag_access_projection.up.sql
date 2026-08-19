CREATE TABLE tag_access_projection (
    vibes_user_id TEXT NOT NULL,
    vibes_workspace_id TEXT NOT NULL,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    account_epoch BIGINT NOT NULL,
    membership_generation BIGINT NOT NULL,
    authority_version BIGINT NOT NULL,
    last_event_id TEXT NOT NULL DEFAULT '',
    last_payload_digest BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_projection_role_check CHECK (role IN ('owner', 'admin', 'member')),
    CONSTRAINT tag_access_projection_status_check CHECK (status IN ('active', 'removed', 'disabled')),
    CONSTRAINT tag_access_projection_account_epoch_check CHECK (account_epoch > 0),
    CONSTRAINT tag_access_projection_generation_check CHECK (membership_generation > 0),
    CONSTRAINT tag_access_projection_version_check CHECK (authority_version > 0),
    CONSTRAINT tag_access_projection_digest_check CHECK (octet_length(last_payload_digest) = 32)
);
