-- A squad's adaptive attention limits (spec 2.6, plan 9.9). The nightly job
-- writes the computed caps from the squad's decided proposals; an admin may pin
-- other caps, which win until cleared. The proposal limits read the effective
-- caps: pinned if pinned, else computed, else the defaults (3 pending, 3 a week).
--
-- acceptance_rate is approved over approved, rejected and expired, for the
-- decisions of the last 60 days; it is NULL when there is no sample. The
-- workspace cap of ten is not here: it never moves.
--
-- No foreign keys: the workspace and the squad are validated in application
-- code. The primary key is built concurrently in 719 and attached in 720.
CREATE TABLE IF NOT EXISTS squad_attention (
    squad_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    pending_cap INTEGER NOT NULL DEFAULT 3 CHECK (pending_cap BETWEEN 1 AND 10),
    weekly_cap INTEGER NOT NULL DEFAULT 3 CHECK (weekly_cap BETWEEN 1 AND 10),
    acceptance_rate NUMERIC(5, 4) CHECK (acceptance_rate BETWEEN 0 AND 1),
    sample_size INTEGER NOT NULL DEFAULT 0 CHECK (sample_size >= 0),
    pinned BOOLEAN NOT NULL DEFAULT false,
    pinned_pending_cap INTEGER CHECK (pinned_pending_cap BETWEEN 1 AND 10),
    pinned_weekly_cap INTEGER CHECK (pinned_weekly_cap BETWEEN 1 AND 10),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (NOT pinned OR (pinned_pending_cap IS NOT NULL AND pinned_weekly_cap IS NOT NULL))
);
