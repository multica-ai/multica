CREATE TABLE weixin_login_session (
    id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    initiator_user_id uuid NOT NULL,
    state_encrypted bytea NOT NULL,
    status text NOT NULL DEFAULT 'wait',
    installation_id uuid,
    lease_token uuid,
    lease_expires_at timestamptz,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE weixin_account_state (
    installation_id uuid NOT NULL,
    bot_id text NOT NULL,
    user_id text NOT NULL,
    get_updates_buf text NOT NULL DEFAULT '',
    context_token_encrypted bytea NOT NULL DEFAULT '',
    context_time_ms bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);
