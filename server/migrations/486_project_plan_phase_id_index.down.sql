-- Remove phase identity, row locking, and endpoint validation support.
DROP INDEX CONCURRENTLY IF EXISTS idx_project_plan_phase_id;
