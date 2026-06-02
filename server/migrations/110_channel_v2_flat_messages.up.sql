-- Channel V2: flat messages + implicit threads on reply.
-- thread_id becomes nullable (NULL = top-level channel message).
-- reply_to_id links a reply to the message it responds to.
-- root_message_id on thread points to the originating top-level message.

ALTER TABLE channel_message ALTER COLUMN thread_id DROP NOT NULL;

ALTER TABLE channel_message ADD COLUMN reply_to_id UUID REFERENCES channel_message(id) ON DELETE SET NULL;

ALTER TABLE channel_thread ADD COLUMN root_message_id UUID REFERENCES channel_message(id) ON DELETE SET NULL;

-- Index for listing top-level channel messages efficiently.
CREATE INDEX idx_channel_message_channel_toplevel ON channel_message(channel_id, created_at ASC) WHERE thread_id IS NULL;

-- Index for finding replies to a specific message.
CREATE INDEX idx_channel_message_reply_to ON channel_message(reply_to_id) WHERE reply_to_id IS NOT NULL;

-- Index for finding thread by root message.
CREATE INDEX idx_channel_thread_root_message ON channel_thread(root_message_id) WHERE root_message_id IS NOT NULL;
