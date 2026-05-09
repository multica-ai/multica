-- 116_channels: Add channel, channel_member, channel_message for multi-party discussion.
-- Channels coexist with existing chat (1:1) and issues (structured execution).
-- Channel → Issue conversion uses a one-way handoff with 3 nullable columns on issue.

-- channel: workspace-scoped discussion room
CREATE TABLE channel (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL DEFAULT 'public' CHECK (type IN ('public', 'private', 'dm')),
    created_by UUID NOT NULL REFERENCES "user"(id),
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name)
);

CREATE INDEX idx_channel_workspace ON channel(workspace_id);
CREATE INDEX idx_channel_archived ON channel(workspace_id, archived_at);

-- channel_member: humans and agents are equal participants
CREATE TABLE channel_member (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    member_type TEXT NOT NULL CHECK (member_type IN ('user', 'agent')),
    member_id UUID NOT NULL,
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'member')),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(channel_id, member_type, member_id)
);

CREATE INDEX idx_channel_member_channel ON channel_member(channel_id);
CREATE INDEX idx_channel_member_member ON channel_member(member_type, member_id);

-- channel_message: messages with thread support and conversion tracking
CREATE TABLE channel_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    author_type TEXT NOT NULL CHECK (author_type IN ('user', 'agent', 'system')),
    author_id UUID NOT NULL,
    content TEXT NOT NULL,
    thread_root_id UUID REFERENCES channel_message(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'converted', 'deleted')),
    metadata JSONB NOT NULL DEFAULT '{}',
    edited_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_message_channel ON channel_message(channel_id, created_at);
CREATE INDEX idx_channel_message_thread ON channel_message(thread_root_id);
CREATE INDEX idx_channel_message_status ON channel_message(status);

-- channel_read_state: per-member read cursor
CREATE TABLE channel_read_state (
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    member_type TEXT NOT NULL,
    member_id UUID NOT NULL,
    last_read_message_id UUID REFERENCES channel_message(id),
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, member_type, member_id)
);

-- issue: add 3 nullable columns for channel → issue conversion
ALTER TABLE issue ADD COLUMN source_channel_id UUID REFERENCES channel(id);
ALTER TABLE issue ADD COLUMN source_thread_root_id UUID REFERENCES channel_message(id);
ALTER TABLE issue ADD COLUMN discussion_snapshot JSONB;
