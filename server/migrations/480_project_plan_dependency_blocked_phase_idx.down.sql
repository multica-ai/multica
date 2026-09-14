-- Remove blocked-phase dependency lookup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_dependency_blocked_phase_idx;
