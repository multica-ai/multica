-- Durable proof of the original realtime participants captured for an exact
-- #288 close command. Retries must never replace this set with only the boots
-- whose Redis leases remain live later.
CREATE TABLE tag_access_connection_close_dispatch (
    command_id TEXT NOT NULL,
    source TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    vibes_workspace_id TEXT NOT NULL DEFAULT '',
    authority_version BIGINT NOT NULL DEFAULT 0,
    identity_restriction_version BIGINT NOT NULL DEFAULT 0,
    session_workspace_generation BIGINT NOT NULL DEFAULT 0,
    target_digest TEXT NOT NULL,
    participant_instance_ids TEXT[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_close_dispatch_source_check CHECK (source IN ('workspace_projection', 'identity_restriction', 'session_workspace_supersession')),
    CONSTRAINT tag_access_close_dispatch_authority_version_check CHECK (authority_version >= 0),
    CONSTRAINT tag_access_close_dispatch_identity_version_check CHECK (identity_restriction_version >= 0),
    CONSTRAINT tag_access_close_dispatch_session_generation_check CHECK (session_workspace_generation >= 0),
    CONSTRAINT tag_access_close_dispatch_participants_check CHECK (cardinality(participant_instance_ids) > 0)
);

-- Durable proof that the exact #288 close command completed on every captured
-- realtime participant. This is receipt evidence, never VIBES authority state.
CREATE TABLE tag_access_connection_close_receipt (
    receipt_id TEXT NOT NULL,
    source TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    vibes_workspace_id TEXT NOT NULL DEFAULT '',
    authority_version BIGINT NOT NULL DEFAULT 0,
    identity_restriction_version BIGINT NOT NULL DEFAULT 0,
    session_workspace_generation BIGINT NOT NULL DEFAULT 0,
    target_digest TEXT NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_close_receipt_source_check CHECK (source IN ('workspace_projection', 'identity_restriction', 'session_workspace_supersession')),
    CONSTRAINT tag_access_close_receipt_authority_version_check CHECK (authority_version >= 0),
    CONSTRAINT tag_access_close_receipt_identity_version_check CHECK (identity_restriction_version >= 0),
    CONSTRAINT tag_access_close_receipt_session_generation_check CHECK (session_workspace_generation >= 0)
);
