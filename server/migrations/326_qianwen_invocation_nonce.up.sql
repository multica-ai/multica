-- Durable replay fence for signed Qianwen pairing invocations. The keyed
-- digest covers timestamp + nonce, while request_digest lets a provider retry
-- the same semantic request with fresh transport fields and recover its result.
CREATE TABLE qianwen_invocation_nonce (
    installation_id UUID NOT NULL,
    nonce_digest    BYTEA NOT NULL,
    request_digest  BYTEA NOT NULL,
    outcome         TEXT,
    multica_user_id UUID,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(nonce_digest) = 32),
    CHECK (octet_length(request_digest) = 32),
    CHECK (outcome IS NULL OR outcome IN ('paired', 'code_invalid')),
    CHECK ((outcome = 'paired') = (multica_user_id IS NOT NULL)),
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + INTERVAL '5 minutes')
);

COMMENT ON TABLE qianwen_invocation_nonce IS
    'Short-lived signed-request digests and outcomes for replay-safe Qianwen idempotency.';
