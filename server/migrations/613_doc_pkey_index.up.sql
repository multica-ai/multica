-- Backing index for doc's primary key, attached in 614.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS doc_pkey_uidx
    ON doc (id);
