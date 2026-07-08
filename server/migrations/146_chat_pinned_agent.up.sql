-- Per-user "quick agent" pins for the Chat list: a curated, ordered set of
-- agents the user keeps at the top of the conversation list for one-tap new
-- chats. Kept separate from the generic `pinned_item` table (issues/projects)
-- so it doesn't leak into the sidebar's pinned section.
CREATE TABLE chat_pinned_agent (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    position FLOAT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id, agent_id)
);

CREATE INDEX idx_chat_pinned_agent_user_ws ON chat_pinned_agent (workspace_id, user_id, position);
