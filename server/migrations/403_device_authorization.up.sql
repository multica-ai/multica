CREATE TABLE device_authorization (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_code TEXT NOT NULL UNIQUE,
    user_code_hash TEXT NOT NULL,
    user_id UUID REFERENCES "user"(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    token TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    interval_seconds INT NOT NULL DEFAULT 5,
    last_polled_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Pending lookups from the approval endpoint; approved rows are fetched by
-- device_code (unique) from the CLI polling side.
CREATE INDEX idx_device_authorization_user_code ON device_authorization(user_code_hash) WHERE status = 'pending';
