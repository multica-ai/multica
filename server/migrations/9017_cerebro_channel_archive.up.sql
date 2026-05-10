-- Per-(channel × user) archive flag. Channels are issues with kind in
-- ('channel', 'dm'); a row here means the user has hidden the channel
-- from their inbox until something happens that re-surfaces it. The
-- absence of a row means the channel is visible (the default).
CREATE TABLE IF NOT EXISTS cerebro_channel_archived (
    channel_id  uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    archived_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX IF NOT EXISTS cerebro_channel_archived_user_idx
    ON cerebro_channel_archived (user_id);
