-- SQL mirror of issuestatus.Effective (MUL-6243).
--
-- Several queries have to ask "is this issue terminal / open?" in SQL, where
-- the Go resolver cannot reach. Without this, a custom status in the `done`
-- category would still count as open for the duplicate guard, and would be
-- missed by sub-issue and project completion counts.
--
-- The built-in fast path comes FIRST and short-circuits, so a workspace with no
-- custom statuses performs no catalog lookup at all — the same guarantee the Go
-- resolver gives. The lookup for a custom key is a single point read on
-- idx_issue_status_workspace_key.
--
-- Falls back to the raw status when the key is unknown or its category is
-- corrupt, matching the Go resolver's fail-safe direction: an unrecognized
-- status matches none of the canonical comparisons, so the issue is left alone
-- rather than being treated as terminal on a guess.
--
-- STABLE (not IMMUTABLE): the result depends on catalog rows, so it must not be
-- folded into an index or cached across statements.
CREATE FUNCTION issue_effective_status(p_workspace_id UUID, p_status TEXT)
RETURNS TEXT
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
    SELECT CASE
        WHEN p_status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')
            THEN p_status
        ELSE COALESCE(
            (SELECT s.category
               FROM issue_status s
              WHERE s.workspace_id = p_workspace_id
                AND s.key = p_status
                AND s.category IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')),
            p_status)
    END
$$;
