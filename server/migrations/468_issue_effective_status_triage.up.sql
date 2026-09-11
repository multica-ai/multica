-- Teach the SQL mirror of issuestatus.Effective about the reserved `triage`
-- key (MUL-7212, design MUL-7189 §2.1). Like the Go resolver, it is its own
-- category and never looked up: migration 467 guarantees no catalog row can
-- hold it, so the fast path is exact rather than a guess.
CREATE OR REPLACE FUNCTION issue_effective_status(p_workspace_id UUID, p_status TEXT)
RETURNS TEXT
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
    SELECT CASE
        WHEN p_status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled', 'triage')
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
