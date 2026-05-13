CREATE TABLE lark_invitation_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invitation_id UUID NOT NULL REFERENCES workspace_invitation(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tenant_key TEXT NOT NULL,
    invitee_email TEXT NOT NULL,
    lark_open_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'skipped')),
    dedupe_key TEXT NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    sent_message_id TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(dedupe_key)
);

CREATE INDEX idx_lark_invitation_delivery_pending
    ON lark_invitation_delivery(next_attempt_at, created_at)
    WHERE status = 'pending';
