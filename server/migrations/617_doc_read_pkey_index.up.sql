-- Backing index for doc_read's primary key, attached in 618.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS doc_read_pkey_uidx
    ON doc_read (doc_id, user_id);
