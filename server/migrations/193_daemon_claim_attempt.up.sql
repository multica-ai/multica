-- Durable idempotency receipt for machine-level daemon claims. The receipt
-- stores only request metadata; sensitive task payloads and plaintext mat_
-- tokens are rebuilt from the task rows on replay.
--
-- There are deliberately no foreign keys. Task/attempt consistency is
-- maintained by the v2 claim transaction, which writes claim_attempt_id on
-- each dispatched task before committing the ready receipt.
CREATE TABLE daemon_claim_attempt (
    id                  UUID        PRIMARY KEY,
    daemon_id           TEXT        NOT NULL,
    principal_key       TEXT        NOT NULL,
    request_fingerprint TEXT        NOT NULL,
    runtime_ids         UUID[]      NOT NULL,
    max_tasks           INTEGER     NOT NULL,
    status              TEXT        NOT NULL DEFAULT 'processing',
    expires_at          TIMESTAMPTZ NOT NULL,
    ready_at            TIMESTAMPTZ,
    acknowledged_at     TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_daemon_claim_attempt_max_tasks
        CHECK (max_tasks >= 0),
    CONSTRAINT chk_daemon_claim_attempt_status
        CHECK (status IN ('processing', 'ready', 'acknowledged', 'expired'))
);

ALTER TABLE agent_task_queue
    ADD COLUMN claim_attempt_id UUID,
    ADD COLUMN claim_attempt_ordinal INTEGER;
