-- No-op: we cannot distinguish rows backfilled by this migration from rows
-- that legitimately reached 'imported' through the normal flow, and the
-- column itself is preserved by the MUL-2438 refactor. Reverting would
-- either be lossy or wrong.
SELECT 1;
