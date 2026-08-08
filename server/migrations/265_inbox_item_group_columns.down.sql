-- Safe to drop: with the write gate closed nothing populates these, and the
-- legacy read/archived path never consults them.
ALTER TABLE inbox_item
    DROP COLUMN IF EXISTS group_id,
    DROP COLUMN IF EXISTS event_seq,
    DROP COLUMN IF EXISTS target_kind,
    DROP COLUMN IF EXISTS target_id,
    DROP COLUMN IF EXISTS delivery_key;
