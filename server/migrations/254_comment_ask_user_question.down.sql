-- Revert 253: drop the metadata column and its CHECKs, and restore the
-- original 4-value type CHECK.
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_metadata_size_limit;
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_metadata_is_object;
ALTER TABLE comment DROP COLUMN IF EXISTS metadata;

ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_type_check
    CHECK (type IN ('comment', 'status_change', 'progress_update', 'system'));
