-- 9107_cerebro_connection_default_access: per-connection default access mode.
--
-- FIR-2166 "C" v2. A connection now carries the default Allow/Ask/Deny verdict
-- that applies to an actor with NO explicit per-actor tool-policy row, chosen
-- when the connection is created or edited:
--
--   - 'deny'  — off by default; open only to actors explicitly granted Allow
--               (the safe default; used for the secrets-box connection).
--   - 'ask'   — every call pauses for human approval unless a per-actor Allow
--               grants it (a system actor with no human resolves Ask -> Deny).
--   - 'allow' — open to everyone unless a per-actor Deny blocks them (the
--               historical baseline; fine for harmless APIs).
--
-- Honored by toolpolicy.ConnectionEndpointEffective for API-type connections:
-- per-actor Deny wins, then per-actor Allow grants, then this connection
-- default. Default 'deny' is the safe choice for any existing row (e.g. the
-- infisical-admin secrets connection) and for new connections that omit it.

ALTER TABLE workspace_connection
    ADD COLUMN IF NOT EXISTS default_access TEXT NOT NULL DEFAULT 'deny'
        CHECK (default_access IN ('allow', 'ask', 'deny'));
