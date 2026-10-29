-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE conversation_message
    ADD CONSTRAINT conversation_message_pkey PRIMARY KEY USING INDEX conversation_message_pkey_uidx;
