-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE issue_status
    ADD CONSTRAINT issue_status_pkey PRIMARY KEY USING INDEX issue_status_pkey_uidx;
