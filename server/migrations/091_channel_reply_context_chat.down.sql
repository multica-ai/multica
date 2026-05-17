ALTER TABLE channel_reply_context
    DROP CONSTRAINT IF EXISTS channel_reply_context_pkey,
    ADD PRIMARY KEY (connection_id, external_user_id);
