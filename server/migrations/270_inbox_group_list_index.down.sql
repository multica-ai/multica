-- Single statement: DROP INDEX CONCURRENTLY cannot run inside a transaction.
DROP INDEX CONCURRENTLY IF EXISTS inbox_group_recipient_surfaced_idx;
