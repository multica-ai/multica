-- The child-done system rule used to run only after the status write had
-- committed, best-effort: a failed or interrupted follow-up lost the parent's
-- wake. The status write now records the transition in its own transaction,
-- for every writer (HTTP, batch, PR automation, any future path). The server
-- processes the row right after commit and a scheduler sweep retries rows that
-- were not processed. No foreign keys; issue and workspace deletion remove
-- these rows in the application deletion graph.
CREATE TABLE IF NOT EXISTS issue_child_done_event (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL,
 parent_id uuid NOT NULL,
 child_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 claimed_at timestamptz,
 processed_at timestamptz
);

-- A child enters a closed status: built-in done/cancelled, or a custom
-- status in the done or closed category. Leaving and re-entering records a
-- new transition, as before.
CREATE OR REPLACE FUNCTION record_issue_child_done() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=NEW.workspace_id AND s.key=NEW.status AND s.category IN ('done','closed')))
  AND NOT (OLD.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=OLD.workspace_id AND s.key=OLD.status AND s.category IN ('done','closed'))) THEN
  INSERT INTO issue_child_done_event(workspace_id,parent_id,child_id) VALUES(NEW.workspace_id,NEW.parent_issue_id,NEW.id);
 END IF;
 RETURN NULL;
END $$;

DROP TRIGGER IF EXISTS issue_child_done_event_trigger ON issue;
CREATE TRIGGER issue_child_done_event_trigger AFTER UPDATE OF status ON issue FOR EACH ROW
 WHEN (NEW.parent_issue_id IS NOT NULL AND OLD.status IS DISTINCT FROM NEW.status)
 EXECUTE FUNCTION record_issue_child_done();
