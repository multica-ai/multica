-- Changes to a parent's set of sub-issues, recorded in the writing transaction
-- for every writer (HTTP, batch, PR automation, any future path). They drive
-- the parent's child_done system wakeup and sub-issue conditions: the server
-- processes the rows right after commit and a scheduler sweep retries rows
-- that were not processed. No foreign keys; issue and workspace deletion
-- remove these rows in the application deletion graph.
--
-- kind: closed | reopened (a sub-issue entered or left a closed status,
-- built-in or custom), attached | detached (it joined or left the parent),
-- restaged (its stage changed).
CREATE TABLE IF NOT EXISTS issue_child_event (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL,
 parent_id uuid NOT NULL,
 child_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('closed','reopened','attached','detached','restaged')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 claimed_at timestamptz,
 processed_at timestamptz
);

CREATE OR REPLACE FUNCTION record_issue_child_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_closed boolean; new_closed boolean;
BEGIN
 IF TG_OP='INSERT' THEN
  INSERT INTO issue_child_event(workspace_id,parent_id,child_id,kind) VALUES(NEW.workspace_id,NEW.parent_issue_id,NEW.id,'attached');
  RETURN NULL;
 END IF;
 IF OLD.parent_issue_id IS DISTINCT FROM NEW.parent_issue_id THEN
  IF OLD.parent_issue_id IS NOT NULL THEN
   INSERT INTO issue_child_event(workspace_id,parent_id,child_id,kind) VALUES(OLD.workspace_id,OLD.parent_issue_id,OLD.id,'detached');
  END IF;
  IF NEW.parent_issue_id IS NOT NULL THEN
   INSERT INTO issue_child_event(workspace_id,parent_id,child_id,kind) VALUES(NEW.workspace_id,NEW.parent_issue_id,NEW.id,'attached');
  END IF;
  RETURN NULL;
 END IF;
 IF OLD.status IS DISTINCT FROM NEW.status THEN
  new_closed := NEW.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=NEW.workspace_id AND s.key=NEW.status AND s.category IN ('done','closed'));
  old_closed := OLD.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=OLD.workspace_id AND s.key=OLD.status AND s.category IN ('done','closed'));
  IF new_closed AND NOT old_closed THEN
   INSERT INTO issue_child_event(workspace_id,parent_id,child_id,kind) VALUES(NEW.workspace_id,NEW.parent_issue_id,NEW.id,'closed');
  ELSIF old_closed AND NOT new_closed THEN
   INSERT INTO issue_child_event(workspace_id,parent_id,child_id,kind) VALUES(NEW.workspace_id,NEW.parent_issue_id,NEW.id,'reopened');
  END IF;
 END IF;
 IF OLD.stage IS DISTINCT FROM NEW.stage THEN
  INSERT INTO issue_child_event(workspace_id,parent_id,child_id,kind) VALUES(NEW.workspace_id,NEW.parent_issue_id,NEW.id,'restaged');
 END IF;
 RETURN NULL;
END $$;

DROP TRIGGER IF EXISTS issue_child_event_insert ON issue;
CREATE TRIGGER issue_child_event_insert AFTER INSERT ON issue FOR EACH ROW
 WHEN (NEW.parent_issue_id IS NOT NULL)
 EXECUTE FUNCTION record_issue_child_event();

DROP TRIGGER IF EXISTS issue_child_event_update ON issue;
CREATE TRIGGER issue_child_event_update AFTER UPDATE OF status,parent_issue_id,stage ON issue FOR EACH ROW
 WHEN (OLD.parent_issue_id IS DISTINCT FROM NEW.parent_issue_id
  OR (NEW.parent_issue_id IS NOT NULL AND (OLD.status IS DISTINCT FROM NEW.status OR OLD.stage IS DISTINCT FROM NEW.stage)))
 EXECUTE FUNCTION record_issue_child_event();
