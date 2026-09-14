-- Remove part position uniqueness and ordered-read support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_part_phase_position_key;
