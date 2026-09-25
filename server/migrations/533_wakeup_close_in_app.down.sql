CREATE OR REPLACE FUNCTION stop_issue_wakeups() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN ('done','cancelled') OR EXISTS
 (SELECT 1 FROM issue_status s WHERE s.workspace_id=NEW.workspace_id AND s.key=NEW.status AND s.category IN ('done','closed')) THEN
  UPDATE issue_wakeup SET enabled=false,disabled_at=clock_timestamp(),updated_at=clock_timestamp()
   WHERE issue_id=OLD.id AND disabled_at IS NULL;
  UPDATE agent_task_queue SET status='cancelled',completed_at=now(),error='Issue closed; wakeup disabled'
   WHERE issue_id=OLD.id AND context->>'wakeup_id' IS NOT NULL AND status IN ('queued','deferred') AND started_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS stop_issue_wakeups ON issue;
CREATE TRIGGER stop_issue_wakeups AFTER UPDATE OF status ON issue FOR EACH ROW EXECUTE FUNCTION stop_issue_wakeups();
