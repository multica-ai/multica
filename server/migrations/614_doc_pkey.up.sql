-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE doc
    ADD CONSTRAINT doc_pkey PRIMARY KEY USING INDEX doc_pkey_uidx;
