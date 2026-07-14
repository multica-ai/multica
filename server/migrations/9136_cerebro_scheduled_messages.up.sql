CREATE TABLE IF NOT EXISTS cerebro_scheduled_message (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    author_user_id uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    content text NOT NULL,
    parent_id uuid REFERENCES comment(id) ON DELETE CASCADE,
    attachment_ids uuid[] NOT NULL DEFAULT '{}',
    send_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'sent', 'failed')),
    sent_comment_id uuid REFERENCES comment(id) ON DELETE SET NULL,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cerebro_scheduled_message_due_idx
    ON cerebro_scheduled_message (send_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS cerebro_scheduled_message_owner_idx
    ON cerebro_scheduled_message (workspace_id, author_user_id, issue_id, send_at)
    WHERE status IN ('pending', 'processing', 'failed');
