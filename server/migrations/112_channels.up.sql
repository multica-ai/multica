CREATE TABLE channel (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  lark_chat_id TEXT,
  created_by UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, name)
);

CREATE INDEX idx_channel_workspace_updated
  ON channel(workspace_id, updated_at DESC);

CREATE TABLE channel_member (
  channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  member_type TEXT NOT NULL CHECK (member_type IN ('user', 'agent')),
  member_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (channel_id, member_type, member_id)
);

CREATE INDEX idx_channel_member_channel
  ON channel_member(channel_id, member_type);

CREATE TABLE channel_agent_session (
  channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
  agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  chat_session_id UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (channel_id, agent_id),
  UNIQUE (chat_session_id)
);

CREATE INDEX idx_channel_agent_session_chat
  ON channel_agent_session(chat_session_id);

CREATE TABLE channel_message (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  author_type TEXT NOT NULL CHECK (author_type IN ('user', 'agent', 'lark', 'system')),
  author_id UUID,
  author_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'multica' CHECK (source IN ('multica', 'lark')),
  external_message_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_message_channel_created
  ON channel_message(channel_id, created_at);

CREATE UNIQUE INDEX idx_channel_message_lark_dedupe
  ON channel_message(channel_id, external_message_id)
  WHERE external_message_id IS NOT NULL;
