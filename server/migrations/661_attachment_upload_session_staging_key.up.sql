-- staging_key is the key the presigned PUT writes, and the only key the
-- reconciler can name when reclaiming a late materialization. Unique so a
-- duplicate-key insert is a detectable bug rather than a second row claiming
-- the same object, and indexed so cleanup can resolve a key back to its
-- session. CONCURRENTLY in its own file per the repo migration convention.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_attachment_upload_session_staging_key
    ON attachment_upload_session (staging_key);
