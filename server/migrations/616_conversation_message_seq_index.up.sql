-- Orders a conversation's messages; pages read by seq.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_message_seq_uidx
    ON conversation_message (conversation_id, seq);
