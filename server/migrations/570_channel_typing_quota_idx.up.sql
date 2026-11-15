CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS channel_typing_reaction_quota_idx ON channel_typing_reaction (workspace_id, quota_slot) WHERE cleaned_at IS NULL AND abandoned_at IS NULL;
