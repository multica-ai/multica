-- Forward no-op: pairs with 763 down, which merges split autopilot_id buckets
-- back into autopilot_id = NULL before 762/761 down rebuild the 8-column
-- unique indexes CONCURRENTLY.
SELECT 1;
