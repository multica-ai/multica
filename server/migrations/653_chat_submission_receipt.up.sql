-- Acceptance receipts outlive message cancellation and attachment deletion.
-- Writers and session/parent deleters hold the chat_session row lock.
CREATE TABLE chat_submission_receipt (
    chat_session_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    initiator_user_id UUID NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id UUID NOT NULL,
    submission_id UUID NOT NULL,
    request_fingerprint BYTEA NOT NULL,
    response JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
