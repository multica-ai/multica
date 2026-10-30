-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE doc_version
    ADD CONSTRAINT doc_version_pkey PRIMARY KEY USING INDEX doc_version_pkey_uidx;
