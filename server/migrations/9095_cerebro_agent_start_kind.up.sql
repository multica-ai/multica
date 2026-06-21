-- FIR-1597: make auto-starting an agent on an issue's start/due date an explicit
-- opt-in instead of "any agent-assigned issue with a start date starts".
--
-- Per-issue choice lives on cerebro_issue_date_time.agent_start_kind:
--   'start' -> auto-start the assigned agent at the start_date (+ start_time)
--   'due'   -> auto-start the assigned agent at the due_date  (+ due_time)
--   'none'  -> never auto-start this issue (explicit override of a workspace default)
--   NULL    -> inherit the workspace default below
-- A start/due choice always carries a time-of-day (enforced by the handler and
-- the sweeper), so an opted-in issue fires at a deliberate wall-clock moment.
--
-- The workspace-wide default for new issues lives on
-- cerebro_workspace_settings.default_agent_start_kind ('none' by default, so
-- nothing auto-starts until someone opts in).
ALTER TABLE cerebro_issue_date_time
    ADD COLUMN IF NOT EXISTS agent_start_kind TEXT
        CHECK (agent_start_kind IN ('none', 'start', 'due'));

ALTER TABLE cerebro_workspace_settings
    ADD COLUMN IF NOT EXISTS default_agent_start_kind TEXT NOT NULL DEFAULT 'none'
        CHECK (default_agent_start_kind IN ('none', 'start', 'due'));
