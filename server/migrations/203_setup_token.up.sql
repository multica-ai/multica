-- Short-lived, single-use credentials for connecting a daemon from the web UI.
-- The browser mints an mst_ token for one workspace; `multica setup --token`
-- atomically exchanges it for the same 90-day user PAT the browser callback
-- flow creates today. Only the hash is persisted, so a database read cannot
-- recover a command that has been shown to the user.
--
-- The progress columns are deliberately stored on the same short-lived row.
-- They let the browser distinguish command redemption, daemon connectivity,
-- and runtime discovery (including the important connected-with-zero-runtimes
-- state) without inventing a second durable device/session model.
--
-- No foreign keys: user/workspace membership is revalidated at redemption and
-- dependent cleanup is owned by the member/workspace delete transactions.
CREATE TABLE setup_token (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    redeemed_at TIMESTAMPTZ,
    daemon_connected_at TIMESTAMPTZ,
    daemon_id TEXT,
    runtime_count INTEGER NOT NULL DEFAULT 0 CHECK (runtime_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE setup_token IS
    'Short-lived one-time mst_ credentials for non-browser CLI setup, plus observable setup progress. No raw token, FK, or cascade is stored.';
