CREATE TABLE attachment_upload_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL,
    object_key TEXT NOT NULL,
    upload_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    part_size_bytes BIGINT NOT NULL,
    uploader_type TEXT NOT NULL,
    uploader_id UUID NOT NULL,
    issue_id UUID REFERENCES issue(id) ON DELETE CASCADE,
    comment_id UUID REFERENCES comment(id) ON DELETE CASCADE,
    chat_session_id UUID REFERENCES chat_session(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'completed', 'aborted', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (attachment_id),
    UNIQUE (workspace_id, object_key, upload_id)
);

CREATE INDEX idx_attachment_upload_session_workspace_status_expires
    ON attachment_upload_session(workspace_id, status, expires_at);

CREATE INDEX idx_attachment_upload_session_pending_expires
    ON attachment_upload_session(expires_at)
    WHERE status = 'pending';
