CREATE OR REPLACE FUNCTION record_issue_stage_blockers() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE candidate uuid; previous_parent uuid;
BEGIN
 IF TG_OP='UPDATE' THEN previous_parent:=OLD.parent_issue_id; END IF;
 IF TG_OP='UPDATE' AND NEW.parent_issue_id IS NOT DISTINCT FROM OLD.parent_issue_id
  AND NEW.stage IS NOT DISTINCT FROM OLD.stage AND NEW.status IS NOT DISTINCT FROM OLD.status THEN RETURN NULL; END IF;
 IF TG_OP='INSERT' OR NEW.parent_issue_id IS DISTINCT FROM OLD.parent_issue_id OR NEW.stage IS DISTINCT FROM OLD.stage THEN
  PERFORM check_issue_blocker_graph(NEW.workspace_id,NEW.id);
 END IF;
 FOR candidate IN
  SELECT NEW.id UNION
  SELECT b.issue_id FROM issue_blocker b WHERE b.workspace_id=NEW.workspace_id AND b.blocker_issue_id=NEW.id UNION
  SELECT i.id FROM issue i WHERE i.workspace_id=NEW.workspace_id AND (i.parent_issue_id=NEW.parent_issue_id OR i.parent_issue_id=previous_parent) AND i.stage IS NOT NULL
 LOOP
  PERFORM arm_issue_blocker_wakeup(NEW.workspace_id,candidate);
 END LOOP;
 RETURN NULL;
END $$;
