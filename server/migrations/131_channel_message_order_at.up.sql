-- Separate display ordering from content creation time for channel messages.
--
-- created_at is the authoritative "when was this content authored" timestamp:
-- it drives the unread model (has_unread / first_unread / last_activity in
-- ListChannels) and must NOT change when a message is merely re-located.
--
-- order_at is "where does this message sort in its list". It defaults to
-- created_at (so normal messages are unaffected) and is the only column the
-- converge/release move re-stamps (to now()) so a moved message lands as the
-- latest entry in its new location without polluting unread state. See
-- ReparentChannelMessage and the OPE-3457 move feature.
--
-- Indexes mirror the existing created_at ones so the order_at-backed list /
-- pagination / reply queries keep their plans.

ALTER TABLE channel_message ADD COLUMN IF NOT EXISTS order_at timestamptz NOT NULL DEFAULT now();
UPDATE channel_message SET order_at = created_at;

CREATE INDEX IF NOT EXISTS idx_channel_message_channel_toplevel_order
    ON channel_message(channel_id, order_at ASC) WHERE thread_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_channel_message_thread_order
    ON channel_message(thread_id, order_at ASC);
