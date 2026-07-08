-- Per-user space ordering for the sidebar Spaces section (mirrors Linear's
-- SpaceMembership.sortOrder). Fractional (double precision) so a drag can
-- take the midpoint of its neighbors without rewriting sibling rows.
-- The first space in a user's order is also the default space for issue
-- creation when no other context applies.
ALTER TABLE workspace_space_member
    ADD COLUMN sort_order DOUBLE PRECISION NOT NULL DEFAULT 0;

-- Backfill: stable per-user sequence by join time (default space first since
-- it was backfilled earliest in migration 131).
WITH ranked AS (
    SELECT space_id, user_id,
           row_number() OVER (
               PARTITION BY workspace_id, user_id
               ORDER BY created_at ASC, space_id ASC
           )::double precision AS rn
    FROM workspace_space_member
)
UPDATE workspace_space_member m
SET sort_order = ranked.rn
FROM ranked
WHERE m.space_id = ranked.space_id
  AND m.user_id = ranked.user_id;
