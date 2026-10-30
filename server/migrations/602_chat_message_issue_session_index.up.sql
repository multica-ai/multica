-- Lists the issues created from one conversation's messages.
CREATE INDEX CONCURRENTLY IF NOT EXISTS chat_message_issue_session_idx
    ON chat_message_issue (chat_session_id);
