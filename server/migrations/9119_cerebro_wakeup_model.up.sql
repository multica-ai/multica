-- FIR-2679 Spor 1b: optional per-wakeup model override. A wakeup can dispatch
-- its run on a cheaper model (e.g. Haiku) for a pure verification check like
-- "is CI green?" so a status ping never fires Opus. Empty string = no override
-- = run on the agent's own model, matching agent_task_queue.model_override and
-- autopilot.model semantics. The value is validated against
-- autopilotmodel.Allowed() at create time; the column itself stays free TEXT so
-- adding a model remains a one-line code change, not a migration.
ALTER TABLE cerebro_agent_wakeup
    ADD COLUMN IF NOT EXISTS model_override TEXT NOT NULL DEFAULT '';
