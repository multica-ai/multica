CREATE TABLE IF NOT EXISTS authority_nonce (
    nonce_hash BYTEA PRIMARY KEY,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (octet_length(nonce_hash) = 32),
    CHECK (expires_at > claimed_at)
);

CREATE INDEX IF NOT EXISTS idx_authority_nonce_expires_at
    ON authority_nonce(expires_at);
