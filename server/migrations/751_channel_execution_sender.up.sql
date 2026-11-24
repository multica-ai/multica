ALTER TABLE chat_message ADD COLUMN channel_sender_user_id UUID, ADD COLUMN channel_source_message_id TEXT, ADD COLUMN channel_source_thread_id TEXT, ADD COLUMN channel_source_sender_id TEXT;
