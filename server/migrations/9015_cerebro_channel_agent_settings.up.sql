-- Per (channel × agent) listen-mode toggle. Channels are issues with
-- kind IN ('channel', 'dm'); the absence of a row defaults to 'always',
-- so an agent newly added to a channel listens to every comment without
-- needing an explicit row. The user flips to 'mention_only' when they
-- want the agent to wait for an @-mention.
CREATE TABLE IF NOT EXISTS cerebro_channel_agent_settings (
    channel_id  uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    agent_id    uuid NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    listen_mode text NOT NULL CHECK (listen_mode IN ('always', 'mention_only')),
    updated_at  timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_channel_agent_settings_agent
    ON cerebro_channel_agent_settings (agent_id);
