-- Immutable authenticated #299 deliveries used for ordered dedupe and conflict
-- evidence. VIBES remains the only producer and authority.
CREATE TABLE tag_access_session_workspace_delivery (
    vibes_user_id TEXT NOT NULL,
    vibes_session_id TEXT NOT NULL,
    session_workspace_generation BIGINT NOT NULL,
    event_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    previous_workspace_id TEXT NOT NULL,
    new_workspace_id TEXT NOT NULL,
    account_epoch BIGINT NOT NULL,
    identity_restriction_version BIGINT NOT NULL,
    authority_version BIGINT NOT NULL,
    membership_generation BIGINT NOT NULL,
    payload_digest BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_session_workspace_delivery_generation_check CHECK (session_workspace_generation >= 2),
    CONSTRAINT tag_access_session_workspace_delivery_epoch_check CHECK (account_epoch > 0),
    CONSTRAINT tag_access_session_workspace_delivery_identity_version_check CHECK (identity_restriction_version >= 0),
    CONSTRAINT tag_access_session_workspace_delivery_authority_version_check CHECK (authority_version > 0),
    CONSTRAINT tag_access_session_workspace_delivery_membership_generation_check CHECK (membership_generation > 0),
    CONSTRAINT tag_access_session_workspace_delivery_workspaces_check CHECK (previous_workspace_id <> new_workspace_id),
    CONSTRAINT tag_access_session_workspace_delivery_digest_check CHECK (octet_length(payload_digest) = 32)
);
