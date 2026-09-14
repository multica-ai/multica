-- Restore migration 469's body, whose fast path knows only the 7 built-in keys.
CREATE OR REPLACE FUNCTION issue_effective_status(p_workspace_id UUID, p_status TEXT)
RETURNS TEXT LANGUAGE sql STABLE PARALLEL SAFE AS $function$
    SELECT CASE
        WHEN p_status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled') THEN p_status
        ELSE COALESCE((SELECT CASE s.category
            WHEN 'done' THEN 'done'
            WHEN 'closed' THEN 'cancelled'
            ELSE p_status END
            FROM issue_status s WHERE s.workspace_id = p_workspace_id AND s.key = p_status), p_status)
    END
$function$;
