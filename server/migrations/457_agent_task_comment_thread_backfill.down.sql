-- The 457 backfill is intentionally irreversible: it restored derived fence
-- data (comment_thread_id is derived, not a FK) on legacy rows to the values
-- the 451 trigger would have written. Re-NULLing them would reopen the
-- pending-task fence gap the backfill closed, so there is nothing to roll
-- back. Explicit no-op.
SELECT 1;
