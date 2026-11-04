-- Budgets (spec 4.2): a spend limit for a scope and a period, the per-period
-- counters the claim gate reads, and one row per run the gate held.
--
-- No foreign keys: scope_id, budget_id and task_id are checked by the writers.
-- Primary keys follow the 741/742 pattern (index first, attached after) so
-- every index is built CONCURRENTLY.
--
-- agent_task_queue.budget_accounted_micros is the cost of the task that the
-- accounting job has already added to its periods. The job adds only the
-- difference, so a task read twice or by two runs is counted once. A constant
-- default is a metadata-only change.
CREATE TABLE IF NOT EXISTS budget (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('company', 'floor', 'squad', 'initiative')),
    scope_id UUID,
    period TEXT NOT NULL CHECK (period IN ('week', 'month', 'quarter')),
    amount_micros BIGINT NOT NULL CHECK (amount_micros >= 0),
    soft_percent INTEGER NOT NULL DEFAULT 80 CHECK (soft_percent BETWEEN 1 AND 100),
    mode TEXT NOT NULL DEFAULT 'hold' CHECK (mode IN ('warn', 'hold')),
    classes TEXT[] NOT NULL DEFAULT '{routine,delegated}',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS budget_period (
    budget_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    spent_micros BIGINT NOT NULL DEFAULT 0,
    runs INTEGER NOT NULL DEFAULT 0,
    extended_micros BIGINT NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'ok' CHECK (state IN ('ok', 'soft', 'hard')),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS budget_hold (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    budget_id UUID NOT NULL,
    task_id UUID NOT NULL,
    agent_id UUID,
    squad_id UUID,
    run_class TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at TIMESTAMPTZ,
    release_reason TEXT CHECK (release_reason IS NULL OR release_reason IN (
        'extended', 'period_rolled', 'budget_changed', 'cancelled', 'overridden', 'admitted'))
);

ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS budget_accounted_micros BIGINT NOT NULL DEFAULT 0;
