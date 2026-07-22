-- setup_token backs the one-command runtime connect flow (MUL-5112).
--
-- The web "Connect from the terminal" dialog mints one of these while the
-- user is already signed in, and renders `multica setup --token <mst_...>`.
-- On a headless server the user pastes that single command: the CLI presents
-- the token to POST /api/setup-tokens/exchange, which atomically marks it used
-- and returns a normal 90-day mul_ PAT. No browser round-trip, no localhost
-- callback, no SSH tunnel — the exact path the browser flow cannot serve.
--
-- Security model mirrors GitHub runner registration tokens: short-lived
-- (SetupTokenTTL, minutes) and single-use (used_at is set under an atomic
-- UPDATE ... WHERE used_at IS NULL). The token itself is never a long-lived
-- credential — it only exchanges for one, so a value left in shell history or
-- the clipboard is dead the moment the CLI redeems it or the window lapses.
--
-- workspace_id is the workspace the dialog was open in; the exchange handler
-- publishes setup_token:redeemed to it so the waiting dialog can confirm
-- "command received" the instant the CLI runs, before the daemon registers.
-- No foreign keys per the repo migration rules: the owning user/workspace are
-- resolved in application code, and expired rows are reaped opportunistically
-- on the next mint, so a deleted user/workspace simply leaves a short-lived
-- orphan that lapses on its own.
CREATE TABLE setup_token (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
