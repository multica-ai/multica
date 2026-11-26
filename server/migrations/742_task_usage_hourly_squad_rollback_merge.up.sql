-- Forward no-op: pairs with 742 down, which merges split squad_id buckets back
-- into squad_id = NULL before 741/740 down rebuild the legacy 7-column unique
-- indexes CONCURRENTLY.
SELECT 1;
