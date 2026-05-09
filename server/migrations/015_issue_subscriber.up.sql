-- Issue subscribers: tracks who is subscribed to notifications for an issue
CREATE TABLE issue_subscriber (
    issue_id   UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    user_type  TEXT NOT NULL CHECK (user_type IN ('member', 'agent')),
    user_id    UUID NOT NULL,
    reason     TEXT NOT NULL CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_type, user_id)
);

CREATE INDEX idx_issue_subscriber_user ON issue_subscriber(user_type, user_id);
