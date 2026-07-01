ALTER TABLE self_host_source_channel
  DROP CONSTRAINT IF EXISTS self_host_source_channel_source_other_check;

ALTER TABLE self_host_source_channel
  DROP COLUMN IF EXISTS source_other;
