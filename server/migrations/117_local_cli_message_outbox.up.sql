CREATE TABLE local_cli_message_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL REFERENCES local_cli_message(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES local_cli_run(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'message_side_effects',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'done')),
    attempts INTEGER NOT NULL DEFAULT 0,
    app_origin TEXT,
    last_error TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_local_cli_message_outbox_message_kind
    ON local_cli_message_outbox(message_id, kind);

CREATE INDEX idx_local_cli_message_outbox_due
    ON local_cli_message_outbox(status, next_attempt_at, locked_until, created_at);
