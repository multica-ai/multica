CREATE TABLE auto_subscribe_preference (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    issue_creator BOOLEAN NOT NULL DEFAULT true,
    issue_assignee BOOLEAN NOT NULL DEFAULT true,
    comment_author BOOLEAN NOT NULL DEFAULT true,
    issue_description_mention BOOLEAN NOT NULL DEFAULT false,
    comment_mention BOOLEAN NOT NULL DEFAULT false,
    quick_create_requester BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX idx_auto_subscribe_preference_user
    ON auto_subscribe_preference(user_id, workspace_id);

INSERT INTO auto_subscribe_preference (
    workspace_id,
    user_id,
    issue_creator,
    issue_assignee,
    comment_author,
    issue_description_mention,
    comment_mention,
    quick_create_requester
)
SELECT
    workspace_id,
    user_id,
    true,
    true,
    true,
    true,
    false,
    true
FROM member
ON CONFLICT (workspace_id, user_id) DO NOTHING;
