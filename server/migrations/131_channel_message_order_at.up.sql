-- Separate display ordering from content creation time for channel messages.
--
-- created_at is the authoritative "when was this content authored" timestamp:
-- it drives the unread model (has_unread / first_unread / last_activity in
-- ListChannels) and must NOT change when a message is merely re-located.
--
-- order_at is "where does this message sort in its list". It defaults to
-- created_at (so normal messages are unaffected). ReparentChannelMessage keeps
-- order_at = created_at so a moved message sorts at its original authored time
-- in the new location without polluting unread state (created_at is untouched).
-- Migration 133 backfills rows from an earlier reparent behavior that briefly
-- stamped order_at to now(). See ReparentChannelMessage and OPE-3457.
--
-- Indexes mirror the existing created_at ones so the order_at-backed list /
-- pagination / reply queries keep their plans.

ALTER TABLE channel_message ADD COLUMN IF NOT EXISTS order_at timestamptz NOT NULL DEFAULT now();
UPDATE channel_message SET order_at = created_at;

CREATE INDEX IF NOT EXISTS idx_channel_message_channel_toplevel_order
    ON channel_message(channel_id, order_at ASC) WHERE thread_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_channel_message_thread_order
    ON channel_message(thread_id, order_at ASC);
