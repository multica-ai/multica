-- Drop priority and project_id from autopilot.
-- These fields were never useful in the product: project_id was never exposed in the UI,
-- and priority was redundant with the agent's own task queue priority.

ALTER TABLE autopilot DROP COLUMN IF EXISTS priority;
ALTER TABLE autopilot DROP COLUMN IF EXISTS project_id;
