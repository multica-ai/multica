-- Device authorization flow (`multica login --device`). Rows are short-lived
-- (10 minute TTL, swept an hour after expiry). No foreign key on user_id, per
-- the repo's migration rules; it is set only by an authenticated approval.
-- client_name is the requesting machine's name, shown on /activate before the
-- user confirms. Both codes are stored as SHA-256 hashes (the device code is
-- the CLI's bearer secret), and no token is stored: the login JWT is minted when the CLI
-- collects an approved row. The user_code_hash index is built concurrently in
-- 761.
CREATE TABLE IF NOT EXISTS device_authorization (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_code_hash TEXT NOT NULL UNIQUE,
    user_code_hash TEXT NOT NULL,
    client_name TEXT NOT NULL DEFAULT '',
    user_id UUID,
    status TEXT NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,
    interval_seconds INT NOT NULL DEFAULT 5,
    last_polled_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
