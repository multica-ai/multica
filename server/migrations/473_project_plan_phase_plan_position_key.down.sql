-- Remove phase position uniqueness and ordered-read support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_phase_plan_position_key;
