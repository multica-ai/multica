-- Channels: multi-participant text conversations alongside the issue board.
-- Designed so that visibility, retention, and mention-triggering are enforced
-- in application code, not at the schema layer. Open-string `kind`/`visibility`
-- columns and `metadata` JSONB bags exist so future kinds and per-feature
-- payloads can be added without further migrations.

-- channel: one row per channel or DM.
-- For DMs, `name` is a deterministic participant-set hash and `display_name`
-- is left empty (UI renders from membership).
CREATE TABLE channel (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    -- Today: 'channel' | 'dm'. Future kinds added without schema change.
    kind TEXT NOT NULL,
    -- Today: 'public' | 'private'.
    visibility TEXT NOT NULL,
    -- Polymorphic creator. Mirrors actor_type/actor_id pattern used elsewhere.
    created_by_type TEXT NOT NULL,
    created_by_id UUID NOT NULL,
    -- NULL = inherit workspace.channel_retention_days.
    retention_days INTEGER,
    -- Open metadata bag for sidecar/optional features.
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, kind, name)
);

CREATE INDEX idx_channel_workspace_active
    ON channel(workspace_id)
    WHERE archived_at IS NULL;

CREATE INDEX idx_channel_metadata
    ON channel USING GIN (metadata);

-- channel_membership: polymorphic — `member_type` is 'member' or 'agent'.
-- Deliberately no FK to user(id)/agent(id): Postgres can't express conditional
-- FKs cleanly, and a sidecar may want to allow non-Multica participants.
-- Application code verifies member existence on insert.
CREATE TABLE channel_membership (
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    member_type TEXT NOT NULL,
    member_id UUID NOT NULL,
    role TEXT NOT NULL DEFAULT 'member',
    last_read_message_id UUID,
    last_read_at TIMESTAMPTZ,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by_type TEXT,
    added_by_id UUID,
    notification_level TEXT NOT NULL DEFAULT 'all',
    PRIMARY KEY (channel_id, member_type, member_id)
);

CREATE INDEX idx_channel_membership_member
    ON channel_membership(member_type, member_id);

-- channel_message: a single message in a channel.
-- Threads (Phase 4) use parent_message_id; SET NULL on parent delete keeps
-- orphan replies as top-level instead of cascading.
CREATE TABLE channel_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    author_type TEXT NOT NULL,
    author_id UUID NOT NULL,
    content TEXT NOT NULL,
    parent_message_id UUID REFERENCES channel_message(id) ON DELETE SET NULL,
    edited_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    -- 'user' | 'admin' | 'retention' | 'moderation'. Application-defined.
    deletion_reason TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    content_tsv tsvector
        GENERATED ALWAYS AS (to_tsvector('english', content)) STORED
);

-- Channel timeline. Partial: deleted/retention-purged rows excluded.
CREATE INDEX idx_channel_message_timeline
    ON channel_message(channel_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- Thread view (Phase 4).
CREATE INDEX idx_channel_message_thread
    ON channel_message(parent_message_id, created_at ASC)
    WHERE parent_message_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX idx_channel_message_author
    ON channel_message(author_type, author_id, created_at DESC);

-- Full-text search support (Phase 5).
CREATE INDEX idx_channel_message_tsv
    ON channel_message USING GIN (content_tsv)
    WHERE deleted_at IS NULL;

-- workspace-level retention default + opt-in flag.
-- channels_enabled defaults FALSE so this can be merged "off by default".
ALTER TABLE workspace
    ADD COLUMN channel_retention_days INTEGER;

ALTER TABLE workspace
    ADD COLUMN channels_enabled BOOLEAN NOT NULL DEFAULT FALSE;
