CREATE INDEX CONCURRENTLY IF NOT EXISTS channel_typing_reaction_gc_idx ON channel_typing_reaction (cleaned_at) WHERE cleaned_at IS NOT NULL;
