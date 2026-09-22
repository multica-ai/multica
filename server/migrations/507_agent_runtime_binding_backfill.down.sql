-- Data backfill; the priority-0 rows are indistinguishable from ones a user
-- created, so rollback leaves them in place rather than guessing which to drop.
SELECT 1;
