-- CEREBRO-PATCH(migration-idempotent-009-verification-code): cerebro modification of upstream file
CREATE TABLE IF NOT EXISTS verification_code (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    code TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_verification_code_email ON verification_code(email, used, expires_at);
