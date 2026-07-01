ALTER TABLE self_host_source_channel
  ADD COLUMN source_other TEXT,
  ADD CONSTRAINT self_host_source_channel_source_other_check
    CHECK (
      source_other IS NULL
      OR (
        channel = 'other'
        AND char_length(source_other) <= 512
      )
    );
