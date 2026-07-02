-- Per-agent daily run budget (step 2 of #4076 spend guardrails).
-- NULL = unlimited (default). When set, ClaimTask refuses to dispatch
-- a new task once the agent has already started max_runs_per_day tasks
-- today (UTC midnight boundary). The count is a cheap SUM on
-- agent_task_queue rows created since midnight — no join to task_usage
-- required.
ALTER TABLE agent ADD COLUMN max_runs_per_day INT;
