CREATE TABLE vibes_cli_pat_binding (
    pat_id UUID NOT NULL,
    multica_user_id UUID NOT NULL,
    multica_workspace_id UUID NOT NULL,
    vibes_user_id TEXT NOT NULL,
    vibes_session_id TEXT NOT NULL,
    vibes_workspace_id TEXT NOT NULL,
    tag_session_id TEXT NOT NULL,
    account_epoch BIGINT NOT NULL CHECK (account_epoch >= 1),
    session_workspace_generation BIGINT NOT NULL CHECK (session_workspace_generation >= 1),
    authority_version BIGINT NOT NULL CHECK (authority_version >= 1),
    membership_generation BIGINT NOT NULL CHECK (membership_generation >= 1),
    session_expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
