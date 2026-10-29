-- An issue created from a direct-chat message (MUL-8357). The issue's progress
-- is posted back into the message's thread (thread_session_id), so the link
-- is kept per issue. chat_session_id is the conversation holding the message,
-- read to mark which messages already became issues. No foreign keys by
-- project rule; rows are removed with their chat session in application code.
--
-- issue_id becomes the primary key in 600 from the index built CONCURRENTLY
-- in 599 (repo convention, see 574-575).
CREATE TABLE IF NOT EXISTS chat_message_issue (
    issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    chat_session_id UUID NOT NULL,
    chat_message_id UUID NOT NULL,
    thread_session_id UUID NOT NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
