-- Multica consumer cursor/integrity evidence for VIBES-owned session Workspace
-- generations. This fences Tag grants; it is not a second session authority.
CREATE TABLE tag_access_session_workspace_state (
    vibes_user_id TEXT NOT NULL,
    vibes_session_id TEXT NOT NULL,
    account_epoch BIGINT NOT NULL,
    session_workspace_generation BIGINT NOT NULL,
    observed_session_workspace_generation BIGINT NOT NULL,
    current_workspace_id TEXT NOT NULL,
    integrity_state TEXT NOT NULL,
    blocked_payload_digest BYTEA NOT NULL DEFAULT ''::bytea,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_session_workspace_state_epoch_check CHECK (account_epoch > 0),
    CONSTRAINT tag_access_session_workspace_state_generation_check CHECK (session_workspace_generation > 0),
    CONSTRAINT tag_access_session_workspace_state_observed_check CHECK (observed_session_workspace_generation >= session_workspace_generation),
    CONSTRAINT tag_access_session_workspace_state_integrity_check CHECK (integrity_state IN ('healthy', 'gap', 'conflict')),
    CONSTRAINT tag_access_session_workspace_state_digest_check CHECK (octet_length(blocked_payload_digest) IN (0, 32))
);
