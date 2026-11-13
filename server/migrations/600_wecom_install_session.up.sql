-- Scan-code install sessions for the WeCom smart bot.
--
-- The QR install is a multi-minute, multi-request conversation with WeCom:
-- generate a QR, then poll until an admin scans it and the bot's credentials
-- come back. None of that can live in the request that started it, so the
-- session is a row and a background worker drives it.
--
-- Why the encrypted columns: scode is the polling handle and qr_code_url embeds
-- it, so together they are a bearer credential for "finish creating this bot".
-- They are sealed with the same MULTICA_WECOM_SECRET_KEY that seals the bot
-- secret, which is also why a deployment without that key cannot run the scan
-- flow at all.
--
-- lease_token + lease_expires_at make the poll single-writer across replicas:
-- WeCom rate-limits query_result, and two replicas polling one scode burn that
-- budget twice as fast for no benefit.
--
-- No foreign keys (repo rule): the workspace teardown path deletes these rows
-- explicitly, and installation_id is a plain reference to the row the session
-- produced on success.
CREATE TABLE wecom_install_session (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- request_key_hash is the SHA-256 of the client's idempotency key. Hashed
    -- rather than stored raw so a DB dump does not carry client-chosen strings.
    request_key_hash        TEXT NOT NULL,
    workspace_id            UUID NOT NULL,
    agent_id                UUID NOT NULL,
    initiator_user_id       UUID NOT NULL,
    scode_encrypted         TEXT,
    qr_code_url_encrypted   TEXT,
    status                  TEXT NOT NULL DEFAULT 'creating'
        CHECK (status IN ('creating', 'pending', 'success', 'error')),
    -- poll_after is the earliest next poll. WeCom requires a minimum interval
    -- between query_result calls, so the schedule lives on the row instead of
    -- in a worker's memory, where a restart would lose it.
    poll_after              TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at              TIMESTAMPTZ,
    lease_token             TEXT,
    lease_expires_at        TIMESTAMPTZ,
    installation_id         UUID,
    -- error_reason is a stable machine code the frontend switches on;
    -- error_message is operator-facing detail and is never shown as-is.
    error_reason            TEXT,
    error_message           TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
