-- Add `kind` discriminator to issue so the same table can host both
-- structured issues and channel-style threads. Channels (kind='channel')
-- and direct messages (kind='dm') are full issues underneath: title is
-- the channel name, description is the topic, subscribers are the
-- participants, comments are the messages. Existing rows default to
-- 'issue' so behavior is unchanged.

ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'issue'
    CHECK (kind IN ('issue', 'channel', 'dm'));

CREATE INDEX IF NOT EXISTS idx_issue_kind
    ON issue (workspace_id, kind);
