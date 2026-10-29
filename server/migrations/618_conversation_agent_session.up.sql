-- The hidden chat_session that carries one agent's context in one conversation
-- (a channel's main timeline or one thread). The agent's replies there run on
-- it, so its CLI session resumes per conversation and never mixes contexts.
-- No foreign keys by project rule.
CREATE TABLE IF NOT EXISTS conversation_agent_session (
    conversation_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    chat_session_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
