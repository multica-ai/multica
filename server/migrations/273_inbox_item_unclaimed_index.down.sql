-- Single statement: DROP INDEX CONCURRENTLY cannot run inside a transaction.
DROP INDEX CONCURRENTLY IF EXISTS inbox_item_unclaimed_idx;
