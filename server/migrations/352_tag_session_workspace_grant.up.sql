CREATE TABLE tag_session_workspace_grant (
    tag_session_id TEXT NOT NULL,
    vibes_workspace_id TEXT NOT NULL,
    membership_generation BIGINT NOT NULL,
    authority_version BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_session_workspace_grant_generation_check CHECK (membership_generation > 0),
    CONSTRAINT tag_session_workspace_grant_version_check CHECK (authority_version > 0)
);
