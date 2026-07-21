CREATE TABLE IF NOT EXISTS authority_nonce (
    nonce_hash BYTEA NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (octet_length(nonce_hash) = 32),
    CHECK (expires_at > claimed_at)
);
