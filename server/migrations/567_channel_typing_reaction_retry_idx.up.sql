CREATE INDEX CONCURRENTLY IF NOT EXISTS channel_typing_reaction_retry_idx ON channel_typing_reaction (retry_after, chat_session_id) WHERE cleaned_at IS NULL;
