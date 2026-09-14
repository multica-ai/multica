-- Remove part identity, row locking, and endpoint validation support.
DROP INDEX CONCURRENTLY IF EXISTS idx_project_plan_part_id;
