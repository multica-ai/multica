ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS budget_accounted_micros;
DROP TABLE IF EXISTS budget_hold;
DROP TABLE IF EXISTS budget_period;
DROP TABLE IF EXISTS budget;
