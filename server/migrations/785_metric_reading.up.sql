-- Readings (spec 4.1): one observed value of a metric at a scope. Built-in
-- readings are written nightly by the metric job, manual ones by a member or an
-- agent, webhook ones from outside. Readings are append-only in the product; the
-- retention job (90 days, then one per day) is the only delete.
--
-- No primary key: the series is read by its indexes (786, 787), and an id is
-- only a tiebreaker for the retention job. Each index is its own concurrent
-- migration.
CREATE TABLE IF NOT EXISTS metric_reading (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    metric_key TEXT NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('company', 'floor', 'squad', 'initiative')),
    scope_id UUID,
    value NUMERIC NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('system', 'member', 'agent', 'webhook')),
    actor_id UUID,
    note TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((scope_type = 'company') = (scope_id IS NULL))
);
