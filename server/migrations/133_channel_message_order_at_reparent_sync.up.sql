-- ReparentChannelMessage used to set order_at = now() while preserving created_at,
-- so moved messages showed an old timestamp but sorted as the newest entry. Sync
-- order_at back to created_at for any rows affected by the old behavior.
UPDATE channel_message
SET order_at = created_at
WHERE order_at > created_at;
