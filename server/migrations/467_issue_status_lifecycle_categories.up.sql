-- Forward-only category consolidation (MUL-7240). A single DO statement keeps
-- constraints and the small status catalog rewrite atomic under this runner.
-- Issue keys/IDs, archived rows, and issue associations are never rewritten.
-- No issue updates/events are emitted: migration must not start agent runs.
DO $migration$
BEGIN
    ALTER TABLE issue_status DROP CONSTRAINT IF EXISTS issue_status_system_is_canonical;
    ALTER TABLE issue_status DROP CONSTRAINT IF EXISTS issue_status_category_check;

    UPDATE issue_status SET category = CASE category
        WHEN 'backlog' THEN 'unstarted'
        WHEN 'todo' THEN 'unstarted'
        WHEN 'in_progress' THEN 'started'
        WHEN 'in_review' THEN 'started'
        WHEN 'blocked' THEN 'started'
        WHEN 'cancelled' THEN 'closed'
        ELSE category
    END
    WHERE category NOT IN ('unstarted', 'started', 'done', 'closed');

    ALTER TABLE issue_status ADD CONSTRAINT issue_status_category_check
        CHECK (category IN ('unstarted', 'started', 'done', 'closed'));
    ALTER TABLE issue_status ADD CONSTRAINT issue_status_system_is_canonical CHECK (
        NOT is_system OR (key, category) IN (
            ('backlog', 'unstarted'), ('todo', 'unstarted'),
            ('in_progress', 'started'), ('in_review', 'started'), ('blocked', 'started'),
            ('done', 'done'), ('cancelled', 'closed')
        )
    );

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
END
$migration$;
