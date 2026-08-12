-- Dedicated minimal ledger for the allowlisted private recruitment inbox.
-- source_message_id is the acceptance-approved message ID and is cleared on
-- every terminal transition. structured_summary contains boolean presence/risk
-- flags and counts only; never source text or extracted values.
CREATE TABLE recruitment_inbox_event (
    message_key TEXT NOT NULL,
    source_message_id TEXT NOT NULL,
    structured_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    role_version TEXT NOT NULL DEFAULT '',
    processing_state TEXT NOT NULL CHECK (processing_state IN ('processing', 'replied', 'ignored', 'dead_letter')),
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    sent_message_key TEXT NOT NULL DEFAULT '',
    sent_status TEXT NOT NULL DEFAULT ''
);
