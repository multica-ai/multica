DROP INDEX IF EXISTS idx_self_host_source_channel_domain_md5;

ALTER TABLE self_host_source_channel
  DROP CONSTRAINT IF EXISTS self_host_source_channel_domain_md5_check,
  DROP CONSTRAINT IF EXISTS self_host_source_channel_domain_check,
  DROP COLUMN IF EXISTS domain_md5,
  DROP COLUMN IF EXISTS domain;
