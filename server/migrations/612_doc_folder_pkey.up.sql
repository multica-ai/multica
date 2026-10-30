-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE doc_folder
    ADD CONSTRAINT doc_folder_pkey PRIMARY KEY USING INDEX doc_folder_pkey_uidx;
