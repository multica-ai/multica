-- Append-only failures for a strict rolling ten-minute pairing budget. The
-- redemption transaction holds the installation row FOR UPDATE while counting
-- and appending, so concurrent identities cannot exceed the installation cap.
-- No foreign keys: every transaction revalidates installation authority.
CREATE TABLE qianwen_pairing_attempt (
    installation_id UUID        NOT NULL,
    identity_digest BYTEA       NOT NULL,
    attempted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(identity_digest) = 32)
);

COMMENT ON TABLE qianwen_pairing_attempt IS
    'DB-clock rolling failure log for Qianwen pairing; keyed identity digests only.';
