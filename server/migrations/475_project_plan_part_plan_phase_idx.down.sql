-- Remove plan-wide part scan and same-plan phase validation support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_part_plan_phase_idx;
