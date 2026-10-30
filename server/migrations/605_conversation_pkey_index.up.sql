-- Backing index for conversation's primary key, attached in 605.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_pkey_uidx
    ON conversation (id);
