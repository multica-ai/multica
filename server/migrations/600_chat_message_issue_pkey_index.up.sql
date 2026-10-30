-- Backing index for chat_message_issue's primary key, attached in 600.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS chat_message_issue_pkey_uidx
    ON chat_message_issue (issue_id);
