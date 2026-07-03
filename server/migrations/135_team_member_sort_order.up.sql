-- Per-user team ordering for the sidebar Teams section (mirrors Linear's
-- TeamMembership.sortOrder). Fractional (double precision) so a drag can
-- take the midpoint of its neighbors without rewriting sibling rows.
-- The first team in a user's order is also the default team for issue
-- creation when no other context applies.
ALTER TABLE workspace_team_member
    ADD COLUMN sort_order DOUBLE PRECISION NOT NULL DEFAULT 0;

-- Backfill: stable per-user sequence by join time (default team first since
-- it was backfilled earliest in migration 131).
WITH ranked AS (
    SELECT team_id, user_id,
           row_number() OVER (
               PARTITION BY workspace_id, user_id
               ORDER BY created_at ASC, team_id ASC
           )::double precision AS rn
    FROM workspace_team_member
)
UPDATE workspace_team_member m
SET sort_order = ranked.rn
FROM ranked
WHERE m.team_id = ranked.team_id
  AND m.user_id = ranked.user_id;
