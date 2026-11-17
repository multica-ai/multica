CREATE INDEX CONCURRENTLY IF NOT EXISTS channel_typing_reaction_abandoned_idx ON channel_typing_reaction (abandoned_at) WHERE abandoned_at IS NOT NULL;
