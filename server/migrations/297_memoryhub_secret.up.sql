-- Encrypted secret broker envelope (Plan v1.2 section 12 + v1.3 A6):
-- AES-256-GCM ciphertext plus state/CAS/lease/rotation/redacted-error fields.
-- No inline indexes.
CREATE TABLE memoryhub_secret (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    credential_ref TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('user_key')),
    envelope_version INTEGER NOT NULL,
    key_id TEXT NOT NULL,
    nonce BYTEA NOT NULL,
    ciphertext BYTEA NOT NULL,
    aad TEXT NOT NULL,
    user_key_hash TEXT,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'rotating', 'revoked', 'blocked_migration')),
    state_version INTEGER NOT NULL DEFAULT 1,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_at TIMESTAMPTZ,
    rotation_from_key_id TEXT,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
