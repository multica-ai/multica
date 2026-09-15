-- MUL-7365: expand only. Pending 469 is skipped by cmd/migrate; applied 469
-- remains historical fact. No catalog rows (including archived rows) change.
-- The runner sends this file as one implicit transaction. Bound lock acquisition
-- and execution; NOT VALID avoids a table scan while holding the DDL lock.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

CREATE OR REPLACE FUNCTION issue_status_category(p_category TEXT)
RETURNS TEXT LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $function$
    SELECT CASE p_category
        WHEN 'backlog' THEN 'unstarted' WHEN 'todo' THEN 'unstarted'
        WHEN 'in_progress' THEN 'started' WHEN 'in_review' THEN 'started'
        WHEN 'blocked' THEN 'started' WHEN 'cancelled' THEN 'closed'
        ELSE p_category END
$function$;

ALTER TABLE issue_status
    DROP CONSTRAINT IF EXISTS issue_status_category_check,
    DROP CONSTRAINT IF EXISTS issue_status_system_is_canonical,
    ADD CONSTRAINT issue_status_category_check CHECK (
        category IN ('backlog', 'todo', 'in_progress', 'in_review', 'blocked',
                     'done', 'cancelled', 'unstarted', 'started', 'closed')
    ) NOT VALID,
    ADD CONSTRAINT issue_status_system_is_canonical CHECK (
        NOT is_system OR (key, category) IN (
            ('backlog', 'backlog'), ('backlog', 'unstarted'),
            ('todo', 'todo'), ('todo', 'unstarted'),
            ('in_progress', 'in_progress'), ('in_progress', 'started'),
            ('in_review', 'in_review'), ('in_review', 'started'),
            ('blocked', 'blocked'), ('blocked', 'started'),
            ('done', 'done'), ('cancelled', 'cancelled'), ('cancelled', 'closed')
        )
    ) NOT VALID;

CREATE OR REPLACE FUNCTION issue_effective_status(p_workspace_id UUID, p_status TEXT)
RETURNS TEXT LANGUAGE sql STABLE PARALLEL SAFE AS $function$
    SELECT CASE
        WHEN p_status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled', 'triage') THEN p_status
        ELSE COALESCE((SELECT CASE s.category
            WHEN 'done' THEN 'done'
            WHEN 'cancelled' THEN 'cancelled'
            WHEN 'closed' THEN 'cancelled'
            ELSE p_status END
            FROM issue_status s WHERE s.workspace_id = p_workspace_id AND s.key = p_status), p_status)
    END
$function$;
