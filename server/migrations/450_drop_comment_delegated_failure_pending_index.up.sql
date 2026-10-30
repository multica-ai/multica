-- Migration 445 replaced this index with the smaller unsettled-only partial
-- index after the historical recovery backfill completed.
DROP INDEX CONCURRENTLY IF EXISTS idx_comment_delegated_failure_pending;
