-- Durable schedule declarations for Plugin HTTP hooks. The consented manifest
-- remains the public source of truth; this table is the execution projection
-- that gives each installed hook an activation epoch and a stable scheduler
-- scope without reparsing every installation on every tick.
--
-- Relationships are application-owned by repository policy: install, upgrade,
-- enable/disable, uninstall and workspace teardown reconcile these rows in the
-- same transaction as plugin_installation. No foreign key or cascade is used.
CREATE TABLE plugin_hook_schedule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    hook_key TEXT NOT NULL CHECK (char_length(hook_key) BETWEEN 1 AND 128),
    cron_expression TEXT NOT NULL CHECK (char_length(cron_expression) BETWEEN 1 AND 255),
    timezone TEXT NOT NULL CHECK (char_length(timezone) BETWEEN 1 AND 255),
    generation UUID NOT NULL DEFAULT gen_random_uuid(),
    activated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Display-only projection. Dispatch correctness is recovered from cron +
    -- activated_at + sys_cron_executions even when this value is stale/NULL.
    next_run_at TIMESTAMPTZ,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Schedule attempts use the same operational invocation table as every other
-- Hook trigger. delivery_id is stable across retries; planned_at is the cron
-- occurrence, distinct from created_at (the actual attempt time).
ALTER TABLE plugin_invocation
    ADD COLUMN delivery_id TEXT CHECK (delivery_id IS NULL OR char_length(delivery_id) BETWEEN 1 AND 128),
    ADD COLUMN planned_at TIMESTAMPTZ;

ALTER TABLE plugin_invocation DROP CONSTRAINT IF EXISTS plugin_invocation_trigger_check;
ALTER TABLE plugin_invocation ADD CONSTRAINT plugin_invocation_trigger_check
    CHECK (trigger IN ('ui', 'manual', 'event', 'agent', 'schedule')) NOT VALID;
