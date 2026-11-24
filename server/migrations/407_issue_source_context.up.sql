CREATE TABLE issue_source_context (
    id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    issue_id UUID,
    origin_task_id UUID,
    source_issue_id UUID NOT NULL,
    anchor_comment_id UUID NOT NULL,
    captured_by_user_id UUID NOT NULL,
    snapshot_version SMALLINT NOT NULL,
    snapshot JSONB NOT NULL,
    capture_digest TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'attached', 'abandoned')),
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attached_at TIMESTAMPTZ,
    CHECK (
        (state = 'pending' AND issue_id IS NULL AND origin_task_id IS NOT NULL AND attached_at IS NULL)
        OR (state = 'attached' AND issue_id IS NOT NULL AND attached_at IS NOT NULL)
        OR (state = 'abandoned' AND issue_id IS NULL AND attached_at IS NULL)
    )
);

ALTER TABLE attachment ADD COLUMN source_context_id UUID;

CREATE TABLE issue_source_context_object_intent (
    storage_key TEXT NOT NULL,
    workspace_id UUID NOT NULL,
    source_context_id UUID NOT NULL,
    attachment_id UUID NOT NULL,
    object_url TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'deleting')),
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
