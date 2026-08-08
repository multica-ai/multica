-- Single statement: DROP INDEX CONCURRENTLY cannot run inside a transaction.
DROP INDEX CONCURRENTLY IF EXISTS inbox_item_delivery_key_uidx;
