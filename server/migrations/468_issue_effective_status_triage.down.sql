-- Restore the migration 340 body, whose fast path knows only the 7 canonical
-- keys.
CREATE OR REPLACE FUNCTION issue_effective_status(p_workspace_id UUID, p_status TEXT)
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
