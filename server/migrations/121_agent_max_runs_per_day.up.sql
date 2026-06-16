ALTER TABLE agent ADD COLUMN max_runs_per_day INTEGER NOT NULL DEFAULT 0;
COMMENT ON COLUMN agent.max_runs_per_day IS 'Maximum tasks an agent can be assigned per calendar day (UTC). 0 means no limit.';
