-- FIR-40: per-workspace display-currency preference.
--
-- Currency is a per-workspace choice (Jesper): every member of a workspace sees
-- cost in the workspace's chosen display currency. Stored in a cerebro-owned
-- table rather than patched onto the upstream `workspace` table, so the upstream
-- schema stays untouched. A missing row means "use the default" (USD = today's
-- behavior).
--
-- Cerebro 9xxx namespace + cerebro_ table prefix. Idempotent so the migration
-- test re-run is a no-op.

CREATE TABLE IF NOT EXISTS cerebro_workspace_settings (
    workspace_id     UUID        NOT NULL PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    -- ISO-4217 display currency. CHECK mirrors the supported set in
    -- @multica/cerebro-format-cost so a bad value is a 400, not a constraint 500.
    display_currency TEXT        NOT NULL DEFAULT 'USD' CHECK (display_currency IN ('USD', 'DKK', 'EUR')),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       UUID        REFERENCES "user"(id) ON DELETE SET NULL
);
