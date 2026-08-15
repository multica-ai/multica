-- One active, short-lived pairing code per Qianwen installation and Multica
-- user. There are deliberately no foreign keys: authority is revalidated from
-- channel_installation + member on every write, and a future redeem must do the
-- same. Only the keyed digest is durable; the eight-digit plaintext is returned
-- once by the management API.
CREATE TABLE qianwen_pairing_code (
    installation_id UUID NOT NULL,
    workspace_id    UUID NOT NULL,
    multica_user_id UUID NOT NULL,
    code_digest     BYTEA NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(code_digest) = 32),
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + INTERVAL '10 minutes')
);

COMMENT ON TABLE qianwen_pairing_code IS
    'Current one-time Qianwen pairing-code digest per installation and Multica user; no plaintext and no foreign keys.';

COMMENT ON COLUMN qianwen_pairing_code.code_digest IS
    'HMAC-SHA-256 under a Qianwen-specific key derived from the deployment secret.';
