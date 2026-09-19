ALTER TABLE issue_create_request
    ADD COLUMN deleted_at TIMESTAMPTZ;

ALTER TABLE issue_create_request
    DROP CONSTRAINT issue_create_request_issue_id_fkey;

ALTER TABLE issue_create_request
    ADD CONSTRAINT issue_create_request_issue_id_fkey
    FOREIGN KEY (issue_id) REFERENCES issue(id) ON DELETE SET NULL;

CREATE TABLE comment_create_request (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID NOT NULL,
    request_key TEXT NOT NULL CHECK (length(request_key) BETWEEN 1 AND 200),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, actor_type, actor_id, request_key)
);

INSERT INTO comment_create_request (
    workspace_id, actor_type, actor_id, request_key, payload_sha256, comment_id
)
SELECT workspace_id, author_type, author_id, request_key,
       request_payload_sha256, id
FROM comment
WHERE request_key IS NOT NULL
ON CONFLICT DO NOTHING;
