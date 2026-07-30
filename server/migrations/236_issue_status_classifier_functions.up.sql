-- Single source of truth (SQL side) for classifying an issue status as
-- "closed" vs "completed".  Before this migration the ('done','cancelled',
-- 'archived') and ('done','cancelled') literals were repeated across the sqlc
-- query files, so adding a new status meant hand-auditing every site.  Now the
-- classification is decided in these two functions and every query derives
-- from it.
--
-- Classification (see server/internal/issueguard/issue_status.go and
-- packages/core/issues/config/status.ts for cross-layer mirrors):
--   closed    = done | cancelled | archived
--   completed = done | cancelled             (archived is closed, NOT completed)
--
-- Keep these in lock-step with the Go helpers
-- server/internal/issueguard/issue_status.go (IsClosedStatus /
-- IsCompletedStatus) and the frontend constants in
-- packages/core/issues/config/status.ts (CLOSED_STATUSES / COMPLETED_STATUSES).
--
-- Both functions are IMMUTABLE so they can be inlined by the planner and used
-- freely in any query (including index predicates, should we ever want that).

CREATE OR REPLACE FUNCTION issue_status_is_closed(status text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT status IN ('done', 'cancelled', 'archived')
$$;

CREATE OR REPLACE FUNCTION issue_status_is_completed(status text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT status IN ('done', 'cancelled')
$$;
