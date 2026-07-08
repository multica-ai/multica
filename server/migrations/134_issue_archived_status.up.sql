-- Add 'archived' as a terminal issue status. Like 'cancelled', it is excluded
-- from the board columns (BOARD_STATUSES in packages/core/issues/config/status.ts),
-- so archived issues stay tracked and recoverable but drop out of the active
-- board view. Gives "completed and hidden" a correct semantic home instead of
-- overloading 'cancelled'. See docs/rfcs/hidden-board-status.md (JON-115/JON-117).
--
-- The original constraint is the inline CHECK from 001_init, auto-named
-- issue_status_check by Postgres.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled', 'archived'));
