-- Backing index for doc_folder's primary key, attached in 612.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS doc_folder_pkey_uidx
    ON doc_folder (id);
