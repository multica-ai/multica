ALTER TABLE task_supplement_capability ALTER COLUMN issue_id DROP NOT NULL;
CREATE TABLE chat_supplement (
    message_id UUID NOT NULL,
    task_id UUID NOT NULL,
    chat_session_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    author_id UUID NOT NULL,
    client_request_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'delivering', 'delivered', 'failed', 'unknown')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ
);
