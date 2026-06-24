DROP INDEX IF EXISTS idx_channel_message_thread_order;
DROP INDEX IF EXISTS idx_channel_message_channel_toplevel_order;
ALTER TABLE channel_message DROP COLUMN IF EXISTS order_at;
