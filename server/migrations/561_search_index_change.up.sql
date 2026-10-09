-- Local search index change log (MUL-7754).
--
-- Web and Desktop keep a per-workspace copy of issue, comment, and project text
-- for instant search. Clients catch up by asking which entities changed since
-- the database snapshot they last synced to. `updated_at` cannot answer that:
-- issues and comments are hard-deleted (including cascaded comment deletes), and
-- a timestamp is assigned before commit, so a late-committing writer would be
-- skipped. Triggers record one row per entity with the xid of its latest write;
-- readers compare that xid against pg_snapshot values, which is exact under any
-- commit order. This is the same trigger-populated dirty-key shape as migration
-- 077, and it covers every write path (API, CLI, daemon, agents) at once.
--
-- One row per entity keeps the table bounded: later writes overwrite the xid.
-- Rows carry no content; readers join to the live tables, so a missing row (or
-- a tombstoned comment) means "deleted". No foreign keys: workspace deletion
-- sweeps this table explicitly and skips the triggers via the teardown flag.
--
-- The runner sends this file as one implicit transaction. CREATE TRIGGER takes a
-- short SHARE ROW EXCLUSIVE lock on issue/comment/project, so bound the wait and
-- retry on the next start rather than queue writers behind it.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

CREATE TABLE IF NOT EXISTS search_index_change (
    entity_type  TEXT        NOT NULL CHECK (entity_type IN ('issue', 'comment', 'project')),
    entity_id    UUID        NOT NULL,
    workspace_id UUID        NOT NULL,
    change_xid   xid8        NOT NULL,
    changed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_type, entity_id)
);

-- Highest xid removed by retention pruning. A client whose snapshot could still
-- be missing a pruned change must rebuild its copy instead of catching up.
CREATE TABLE IF NOT EXISTS search_index_prune_mark (
    singleton          BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    pruned_through_xid xid8    NOT NULL
);

-- Keep this function trivial: it runs inside every issue/comment/project write,
-- so an error here would fail the user's write. All inputs are NOT NULL columns.
CREATE OR REPLACE FUNCTION record_search_index_change()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
DECLARE
    changed_id UUID;
    changed_workspace_id UUID;
BEGIN
    IF TG_OP = 'DELETE' THEN
        changed_id := OLD.id;
        changed_workspace_id := OLD.workspace_id;
    ELSE
        changed_id := NEW.id;
        changed_workspace_id := NEW.workspace_id;
    END IF;

    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_xid, changed_at)
    VALUES (TG_ARGV[0], changed_id, changed_workspace_id, pg_current_xact_id(), now())
    ON CONFLICT (entity_type, entity_id) DO UPDATE
        SET workspace_id = EXCLUDED.workspace_id,
            change_xid = EXCLUDED.change_xid,
            changed_at = EXCLUDED.changed_at;
    RETURN NULL;
END
$function$;

-- Every issue column is part of the indexed search result, so any update counts.
DROP TRIGGER IF EXISTS trg_issue_search_index_change ON issue;
CREATE TRIGGER trg_issue_search_index_change
AFTER INSERT OR UPDATE OR DELETE ON issue
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION record_search_index_change('issue');

-- Comments only contribute their text and placement; resolve/revision bumps do
-- not change what search can match.
DROP TRIGGER IF EXISTS trg_comment_search_index_change ON comment;
CREATE TRIGGER trg_comment_search_index_change
AFTER INSERT OR DELETE ON comment
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION record_search_index_change('comment');

DROP TRIGGER IF EXISTS trg_comment_update_search_index_change ON comment;
CREATE TRIGGER trg_comment_update_search_index_change
AFTER UPDATE OF content, deleted_at, issue_id, created_at ON comment
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION record_search_index_change('comment');

DROP TRIGGER IF EXISTS trg_project_search_index_change ON project;
CREATE TRIGGER trg_project_search_index_change
AFTER INSERT OR UPDATE OR DELETE ON project
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION record_search_index_change('project');
