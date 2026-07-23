-- Unique index backing channel_media_pending_object's primary key (attached
-- in 217 USING INDEX) and the intent upsert's ON CONFLICT (storage_key).
-- Kept in its own single-statement migration so CONCURRENTLY runs outside an
-- implicit transaction block (repo convention; see migrations 119 and 208).
CREATE UNIQUE INDEX CONCURRENTLY channel_media_pending_object_storage_key_uidx
    ON channel_media_pending_object (storage_key);
