ALTER TABLE self_host_source_channel
  ADD COLUMN domain TEXT,
  ADD COLUMN domain_md5 TEXT,
  ADD CONSTRAINT self_host_source_channel_domain_check
    CHECK (domain IS NULL OR char_length(domain) <= 255),
  ADD CONSTRAINT self_host_source_channel_domain_md5_check
    CHECK (domain_md5 IS NULL OR domain_md5 ~ '^[0-9a-f]{32}$');

CREATE INDEX idx_self_host_source_channel_domain_md5
  ON self_host_source_channel(domain_md5);
