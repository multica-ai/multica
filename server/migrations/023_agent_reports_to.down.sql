DROP INDEX IF EXISTS idx_agent_reports_to;
ALTER TABLE agent DROP COLUMN IF EXISTS reports_to;
