-- Channel: lightweight human+agent collaboration container (channel + thread + message).
-- A channel is a persistent context container; threads are lightweight reply-scoped
-- conversations inside a channel; issues are produced FROM a thread and reference it back.

CREATE TABLE channel (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- access_mode: 'open' = any workspace member may join; 'invite' = invite-only.
    access_mode TEXT NOT NULL DEFAULT 'open',
    -- is_locked: admin lock — only owners/admins may post / manage when true.
    is_locked BOOLEAN NOT NULL DEFAULT false,
    is_archived BOOLEAN NOT NULL DEFAULT false,
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT channel_access_mode_check CHECK (access_mode IN ('open', 'invite'))
);

CREATE UNIQUE INDEX uniq_channel_workspace_slug ON channel(workspace_id, slug);
CREATE INDEX idx_channel_workspace ON channel(workspace_id, is_archived, updated_at DESC);

CREATE TABLE channel_member (
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id),
    CONSTRAINT channel_member_role_check CHECK (role IN ('owner', 'member'))
);

CREATE INDEX idx_channel_member_user ON channel_member(user_id);

CREATE TABLE channel_thread (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    created_by UUID REFERENCES "user"(id),
    message_count INTEGER NOT NULL DEFAULT 0,
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_thread_channel ON channel_thread(channel_id, last_message_at DESC);

CREATE TABLE channel_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES channel_thread(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- author_type: 'member' | 'agent' | 'system' (system = issue reflow activity).
    author_type TEXT NOT NULL DEFAULT 'member',
    author_id UUID,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT channel_message_author_type_check CHECK (author_type IN ('member', 'agent', 'system'))
);

CREATE INDEX idx_channel_message_thread ON channel_message(thread_id, created_at ASC);

-- Issues produced from a thread keep a back-reference to their source channel/thread.
ALTER TABLE issue ADD COLUMN source_channel_id UUID REFERENCES channel(id) ON DELETE SET NULL;
ALTER TABLE issue ADD COLUMN source_thread_id UUID REFERENCES channel_thread(id) ON DELETE SET NULL;
CREATE INDEX idx_issue_source_thread ON issue(source_thread_id) WHERE source_thread_id IS NOT NULL;
