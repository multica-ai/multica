CREATE TABLE tag_access_workspace_state (
    vibes_workspace_id TEXT NOT NULL,
    authority_version BIGINT NOT NULL DEFAULT 0,
    observed_authority_version BIGINT NOT NULL DEFAULT 0,
    integrity_state TEXT NOT NULL DEFAULT 'gap',
    blocked_payload_digest BYTEA NOT NULL DEFAULT ''::bytea,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_workspace_state_version_check CHECK (authority_version >= 0),
    CONSTRAINT tag_access_workspace_state_observed_check CHECK (observed_authority_version >= authority_version),
    CONSTRAINT tag_access_workspace_state_integrity_check CHECK (integrity_state IN ('healthy', 'gap', 'conflict')),
    CONSTRAINT tag_access_workspace_state_blocked_digest_check CHECK (octet_length(blocked_payload_digest) IN (0, 32))
);
