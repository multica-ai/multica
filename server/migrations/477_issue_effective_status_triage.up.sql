-- Teach the SQL mirror of issuestatus.Effective about the reserved `triage`
-- key (MUL-7212, design MUL-7189 §2.1). Like the Go resolver, it is its own
-- category and never looked up: migrations 475-476 guarantee no catalog row can
-- hold it, so the fast path is exact rather than a guess. The rest of the body
-- is migration 469's, which projects custom terminal categories onto
-- done/cancelled.
CREATE OR REPLACE FUNCTION issue_effective_status(p_workspace_id UUID, p_status TEXT)
RETURNS TEXT LANGUAGE sql STABLE PARALLEL SAFE AS $function$
    SELECT CASE
        WHEN p_status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled', 'triage') THEN p_status
        ELSE COALESCE((SELECT CASE s.category
            WHEN 'done' THEN 'done'
            WHEN 'closed' THEN 'cancelled'
            ELSE p_status END
            FROM issue_status s WHERE s.workspace_id = p_workspace_id AND s.key = p_status), p_status)
    END
$function$;
