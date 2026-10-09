CREATE INDEX CONCURRENTLY IF NOT EXISTS channel_typing_reaction_expiry_idx ON channel_typing_reaction (created_at, id) WHERE cleaned_at IS NULL AND abandoned_at IS NULL;
