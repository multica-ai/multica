-- Metrics (spec 4.1): a named, unit-bearing series that readings are reported
-- against. Built-in metrics are seeded per workspace and computed nightly;
-- manual and webhook metrics are created by their first report. The key is
-- unique per workspace; that index is built concurrently in 784.
--
-- No foreign keys: the workspace is validated by the handlers.
CREATE TABLE IF NOT EXISTS metric (
    workspace_id UUID NOT NULL,
    key TEXT NOT NULL CHECK (key ~ '^[a-z0-9][a-z0-9_.]{0,63}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    unit TEXT NOT NULL DEFAULT '' CHECK (char_length(unit) <= 32),
    source TEXT NOT NULL CHECK (source IN ('builtin', 'manual', 'webhook')),
    direction TEXT NOT NULL DEFAULT 'increase' CHECK (direction IN ('increase', 'decrease')),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
