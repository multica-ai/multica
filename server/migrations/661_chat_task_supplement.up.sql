ALTER TABLE task_supplement_capability ALTER COLUMN issue_id DROP NOT NULL;

CREATE TABLE chat_task_supplement (
    task_id UUID NOT NULL,
    message_id UUID NOT NULL,
    chat_session_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    author_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'delivering', 'delivered', 'requeued', 'withdrawn')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
