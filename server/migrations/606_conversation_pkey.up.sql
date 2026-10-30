-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE conversation
    ADD CONSTRAINT conversation_pkey PRIMARY KEY USING INDEX conversation_pkey_uidx;
