CREATE TABLE agent_feishu_bot_config (
    agent_id UUID PRIMARY KEY REFERENCES agent(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    app_id TEXT NOT NULL,
    app_secret TEXT NOT NULL,
    verification_token TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id)
);

CREATE INDEX idx_agent_feishu_bot_config_workspace
    ON agent_feishu_bot_config(workspace_id);

CREATE TABLE feishu_agent_chat_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id TEXT NOT NULL REFERENCES agent_feishu_bot_config(app_id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    feishu_chat_id TEXT NOT NULL,
    feishu_sender_id TEXT NOT NULL,
    chat_session_id UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id, feishu_chat_id, feishu_sender_id)
);

CREATE INDEX idx_feishu_agent_chat_binding_session
    ON feishu_agent_chat_binding(chat_session_id);

CREATE TABLE feishu_issue_thread (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id TEXT NOT NULL REFERENCES agent_feishu_bot_config(app_id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    feishu_chat_id TEXT NOT NULL,
    feishu_thread_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id, feishu_chat_id, feishu_thread_id)
);

CREATE INDEX idx_feishu_issue_thread_issue
    ON feishu_issue_thread(issue_id);
