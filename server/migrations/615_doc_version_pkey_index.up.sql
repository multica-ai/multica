-- Backing index for doc_version's primary key, attached in 616.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS doc_version_pkey_uidx
    ON doc_version (doc_id, number);
