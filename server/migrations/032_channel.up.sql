-- Channel: workspace-level messaging channel registrations (Slack, Discord, etc.)
CREATE TABLE channel (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    provider      TEXT NOT NULL CHECK (provider IN ('slack')),
    config        JSONB NOT NULL DEFAULT '{}',
    created_by    UUID NOT NULL REFERENCES "user"(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_workspace ON channel(workspace_id);

-- Issue-channel assignment (many-to-many)
CREATE TABLE issue_channel (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id      UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    channel_id    UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    thread_ref    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(issue_id, channel_id)
);

CREATE INDEX idx_issue_channel_issue ON issue_channel(issue_id);
CREATE INDEX idx_issue_channel_channel ON issue_channel(channel_id);

-- Channel messages (agent questions + user responses)
CREATE TABLE channel_message (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_channel_id UUID NOT NULL REFERENCES issue_channel(id) ON DELETE CASCADE,
    direction        TEXT NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    content          TEXT NOT NULL,
    external_id      TEXT,
    sender_type      TEXT NOT NULL CHECK (sender_type IN ('agent', 'user')),
    sender_ref       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_message_issue_channel ON channel_message(issue_channel_id, created_at);
CREATE INDEX idx_channel_message_external ON channel_message(external_id) WHERE external_id IS NOT NULL;
