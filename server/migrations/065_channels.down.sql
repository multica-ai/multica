ALTER TABLE workspace DROP COLUMN IF EXISTS channels_enabled;
ALTER TABLE workspace DROP COLUMN IF EXISTS channel_retention_days;

DROP TABLE IF EXISTS channel_message;
DROP TABLE IF EXISTS channel_membership;
DROP TABLE IF EXISTS channel;
