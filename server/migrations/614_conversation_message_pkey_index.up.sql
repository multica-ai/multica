-- Backing index for conversation_message's primary key, attached in 615.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_message_pkey_uidx
    ON conversation_message (id);
