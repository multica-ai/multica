-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE project_doc
    ADD CONSTRAINT project_doc_pkey PRIMARY KEY USING INDEX project_doc_pkey_uidx;
