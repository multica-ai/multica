-- Remove blocking-phase dependency lookup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_dependency_blocking_phase_idx;
