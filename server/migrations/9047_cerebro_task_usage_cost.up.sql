-- CEREBRO-PATCH(task-usage-gateway-cost): persist the exact spend the Firtal AI
-- gateway returns on every call. Upstream computes per-task cost from tokens ×
-- a pricing table at read time; the managed gateway already knows the real
-- charge (`firtal.cost_cents`), so we store it and prefer it over the estimate
-- on the per-issue / per-session / per-task cost views. Defaults to 0 so
-- daemon/non-gateway runtimes (which report no cost) transparently fall back to
-- the token estimate — no behaviour change for them.
ALTER TABLE task_usage ADD COLUMN IF NOT EXISTS cost_cents BIGINT NOT NULL DEFAULT 0;
