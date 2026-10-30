-- A retried send with the same client id lands once.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_message_client_uidx
    ON conversation_message (conversation_id, author_id, client_message_id)
    WHERE client_message_id IS NOT NULL;
