-- Remove plan identity, row locking, and child validation support.
DROP INDEX CONCURRENTLY IF EXISTS idx_project_plan_id;
