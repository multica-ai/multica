-- Maintains issue.done_at, reopen_count and last_reopened_at (migration 880) on
-- every status change. A trigger rather than a write in each caller, because
-- status is written from many paths (the issue update, batch updates, task
-- completion, GitHub sync, wakeups) and a missed path would silently lose a
-- reopen. "Done" is the same test BuiltinIssuesDone uses: the literal done key
-- or a status whose category is done. A move from done to a closed status
-- (cancelled) is not a reopen: the work was not redone.
CREATE OR REPLACE FUNCTION issue_track_done_transitions() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    old_done BOOLEAN;
    new_done BOOLEAN;
    new_closed BOOLEAN;
BEGIN
    old_done := OLD.status = 'done' OR EXISTS (
        SELECT 1 FROM issue_status st
        WHERE st.workspace_id = OLD.workspace_id AND st.key = OLD.status AND st.category = 'done');
    new_done := NEW.status = 'done' OR EXISTS (
        SELECT 1 FROM issue_status st
        WHERE st.workspace_id = NEW.workspace_id AND st.key = NEW.status AND st.category = 'done');
    new_closed := EXISTS (
        SELECT 1 FROM issue_status st
        WHERE st.workspace_id = NEW.workspace_id AND st.key = NEW.status AND st.category = 'closed');
    IF new_done AND NOT old_done THEN
        NEW.done_at := now();
    END IF;
    IF old_done AND NOT new_done AND NOT new_closed THEN
        NEW.reopen_count := NEW.reopen_count + 1;
        NEW.last_reopened_at := now();
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_issue_track_done_transitions ON issue;
CREATE TRIGGER trg_issue_track_done_transitions
BEFORE UPDATE OF status ON issue
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION issue_track_done_transitions();
