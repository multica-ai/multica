-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE doc_read
    ADD CONSTRAINT doc_read_pkey PRIMARY KEY USING INDEX doc_read_pkey_uidx;
