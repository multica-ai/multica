-- Cerebro: per-autopilot and per-task model override (JEH-1310).
--
-- Today the model an agent runs on is resolved purely at the agent level
-- (server/internal/daemon/daemon.go:2071): agent.model wins, then the
-- daemon-wide MULTICA_<PROVIDER>_MODEL env var, otherwise the CLI's own
-- default. That means simple hourly nudge pings burn the same model as a
-- complex code review — Haiku is 10-50x cheaper than Sonnet/Opus so a per-
-- autopilot override is the highest-leverage cost lever we have.
--
-- This migration adds two NULL-able columns:
--
--   autopilot.model          — optional override for every run of this
--                              autopilot. NULL = fall through to agent.model.
--   agent_task_queue.model_override — per-task override populated by the
--                              autopilot dispatcher (and reused later if we
--                              add per-mention model picks). NULL = no
--                              autopilot override, fall through to agent.model.
--
-- Both columns are deliberately untyped TEXT (not an enum) so a new model
-- ID can ship without a migration — the handler validates against a static
-- registry that the UI and CLI share.

ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS model TEXT;

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS model_override TEXT;
