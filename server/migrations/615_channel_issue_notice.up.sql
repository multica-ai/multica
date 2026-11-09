-- Issue notifications outlive the chat turn that created the issue. They cannot
-- reuse channel_reply_delivery, whose settled turn must never be reopened.
CREATE TABLE channel_issue_notice (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    issue_revision BIGINT NOT NULL,
    event_key TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('completed', 'blocked', 'failed')),
    status_key TEXT NOT NULL,
    failed_task_id UUID,
    source_task_id UUID NOT NULL,
    binding_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    binding_generation TEXT NOT NULL,
    recipient_user_id UUID NOT NULL,
    channel_sender_id TEXT NOT NULL,
    channel_chat_id TEXT NOT NULL,
    channel_message_id TEXT,
    channel_thread_id TEXT,
    route_config JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0,
    owner_token UUID,
    first_attempt_at TIMESTAMPTZ,
    retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    message_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
