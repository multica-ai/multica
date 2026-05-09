-- Add chat session and chat message tables for the agent chat feature.

-- chat_session: persistent chat between a user and an agent.
CREATE TABLE chat_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    creator_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    session_id TEXT,
    work_dir TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_session_workspace ON chat_session(workspace_id);
CREATE INDEX idx_chat_session_creator ON chat_session(creator_id, workspace_id);

-- chat_message: individual messages in a chat session.
CREATE TABLE chat_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    task_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_message_session ON chat_message(chat_session_id, created_at);

-- Make issue_id nullable on agent_task_queue so chat tasks don't need an issue.
ALTER TABLE agent_task_queue ALTER COLUMN issue_id DROP NOT NULL;

-- Add chat_session_id to agent_task_queue for chat tasks.
ALTER TABLE agent_task_queue ADD COLUMN chat_session_id UUID REFERENCES chat_session(id) ON DELETE SET NULL;
