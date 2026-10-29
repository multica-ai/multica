-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE chat_message_issue
    ADD CONSTRAINT chat_message_issue_pkey PRIMARY KEY USING INDEX chat_message_issue_pkey_uidx;
