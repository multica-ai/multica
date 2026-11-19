CREATE TABLE issue_continuation (
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    chain_id UUID NOT NULL,
    remainder_id UUID NOT NULL,
    generation BIGINT NOT NULL CHECK (generation > 0),
    version BIGINT NOT NULL CHECK (version > 0),
    state TEXT NOT NULL CHECK (state IN ('awaiting_event', 'receiver_queued', 'receiver_running', 'resolved', 'needs_reconcile')),
    contract JSONB NOT NULL,
    history JSONB NOT NULL DEFAULT '[]',
    last_progress_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The incident also owns its durable delivery outbox. Its ID is the inbox
-- row ID; the receipt and inbox insert commit in the same database transaction.
CREATE TABLE issue_continuation_incident (
    id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    chain_id UUID NOT NULL,
    remainder_id UUID NOT NULL,
    generation BIGINT NOT NULL CHECK (generation > 0),
    reason TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    terminal_at TIMESTAMPTZ,
    outbox_state TEXT NOT NULL DEFAULT 'pending' CHECK (outbox_state IN ('pending', 'suppressed', 'delivered')),
    notified_at TIMESTAMPTZ,
    last_delivery_error TEXT
);
