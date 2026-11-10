-- Stages are evaluated from the current sibling set, never materialized as edges.
CREATE VIEW effective_issue_blocker AS
 SELECT b.workspace_id,b.issue_id,b.blocker_issue_id,'manual'::text AS via
 FROM issue_blocker b
 UNION ALL
 SELECT i.workspace_id,i.id,s.id,'stage'::text
 FROM issue i JOIN issue s ON s.workspace_id=i.workspace_id
  AND s.parent_issue_id=i.parent_issue_id AND s.stage<i.stage
 WHERE i.parent_issue_id IS NOT NULL
AND NOT EXISTS(SELECT 1 FROM issue_blocker m WHERE m.workspace_id=i.workspace_id AND m.issue_id=i.id AND m.blocker_issue_id=s.id);

CREATE FUNCTION issue_has_open_blockers(ws uuid, target uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM effective_issue_blocker b
 JOIN issue i ON i.id=b.blocker_issue_id AND i.workspace_id=b.workspace_id
 WHERE b.workspace_id=ws AND b.issue_id=target
 AND i.status NOT IN ('done','cancelled')
 AND NOT EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=ws AND s.key=i.status AND s.category IN ('done','closed')))
$$;

ALTER TABLE issue_wakeup DROP CONSTRAINT IF EXISTS issue_wakeup_system_rule_check;
ALTER TABLE issue_wakeup ADD CONSTRAINT issue_wakeup_system_rule_check CHECK (system_rule IN ('child_done','blockers_done'));

-- Record a wait in the writing transaction, including waits shorter than a
-- scheduler tick. A later read determines readiness from the live graph.
CREATE FUNCTION arm_issue_blocker_wakeup(ws uuid, target uuid) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE facts jsonb;
BEGIN
 IF NOT issue_has_open_blockers(ws,target) THEN
  UPDATE issue_wakeup SET next_fire_at=clock_timestamp() WHERE issue_id=target AND workspace_id=ws AND system_rule='blockers_done';
  RETURN;
 END IF;
 SELECT jsonb_agg(jsonb_build_object('id',i.id,'identifier',w.issue_prefix||'-'||i.number,'title',i.title,'status',i.status,'via',b.via,'project_id',i.project_id,'private',EXISTS(SELECT 1 FROM project p WHERE p.id=i.project_id AND p.visibility='private'))) INTO facts
 FROM effective_issue_blocker b JOIN issue i ON i.id=b.blocker_issue_id AND i.workspace_id=ws
 JOIN workspace w ON w.id=ws WHERE b.workspace_id=ws AND b.issue_id=target;
 INSERT INTO issue_wakeup(id,workspace_id,issue_id,instruction,kind,mode,event_types,timezone,condition,condition_state,enabled,system_rule,next_fire_at)
 VALUES(gen_random_uuid(),ws,target,'','event','continuous',ARRAY['issue.status_changed'],'UTC',jsonb_build_object('type','blockers_done','blockers',facts),'',true,'blockers_done',clock_timestamp())
 ON CONFLICT (issue_id,system_rule) WHERE system_rule IS NOT NULL
 DO UPDATE SET condition=EXCLUDED.condition,condition_state='',next_fire_at=clock_timestamp();
END $$;

CREATE FUNCTION lock_issue_blocker_graph() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Opposing concurrent edge writes must see one another before validation.
 PERFORM pg_advisory_xact_lock(hashtextextended('issue-blockers:'||NEW.workspace_id::text,0));
 RETURN NEW;
END $$;
CREATE TRIGGER issue_blocker_graph_lock BEFORE INSERT OR UPDATE ON issue_blocker
 FOR EACH ROW EXECUTE FUNCTION lock_issue_blocker_graph();
CREATE TRIGGER issue_stage_graph_insert BEFORE INSERT ON issue
 FOR EACH ROW WHEN (NEW.parent_issue_id IS NOT NULL AND NEW.stage IS NOT NULL)
 EXECUTE FUNCTION lock_issue_blocker_graph();
CREATE TRIGGER issue_stage_graph_update BEFORE UPDATE OF parent_issue_id,stage ON issue
 FOR EACH ROW WHEN (OLD.parent_issue_id IS DISTINCT FROM NEW.parent_issue_id OR OLD.stage IS DISTINCT FROM NEW.stage)
 EXECUTE FUNCTION lock_issue_blocker_graph();

CREATE FUNCTION check_issue_blocker_graph(ws uuid, target uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(WITH RECURSIVE chain(id) AS (
  SELECT blocker_issue_id FROM effective_issue_blocker WHERE workspace_id=ws AND issue_id=target
  UNION SELECT b.blocker_issue_id FROM effective_issue_blocker b JOIN chain c ON b.issue_id=c.id WHERE b.workspace_id=ws
 ) SELECT 1 FROM chain WHERE id=target) THEN
  RAISE EXCEPTION 'Blockers cannot form a cycle (including stages)' USING ERRCODE='23514',CONSTRAINT='issue_blocker_cycle';
 END IF;
 IF EXISTS(WITH RECURSIVE ancestors(child,ancestor) AS (
  SELECT id,parent_issue_id FROM issue WHERE workspace_id=ws AND parent_issue_id IS NOT NULL
  UNION SELECT a.child,i.parent_issue_id FROM ancestors a JOIN issue i ON i.id=a.ancestor AND i.workspace_id=ws WHERE i.parent_issue_id IS NOT NULL
 ) SELECT 1 FROM issue_blocker b JOIN ancestors a ON
  (a.child=b.issue_id AND a.ancestor=b.blocker_issue_id) OR (a.child=b.blocker_issue_id AND a.ancestor=b.issue_id)
 WHERE b.workspace_id=ws) THEN
  RAISE EXCEPTION 'Parent and child issues cannot block each other' USING ERRCODE='23514',CONSTRAINT='issue_blocker_ancestor';
 END IF;
END $$;

CREATE FUNCTION record_issue_blocker_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ws uuid; target uuid; candidate uuid;
BEGIN
 IF TG_OP='DELETE' THEN ws:=OLD.workspace_id; target:=OLD.issue_id;
 ELSE ws:=NEW.workspace_id; target:=NEW.issue_id;
  PERFORM check_issue_blocker_graph(ws,target);
 END IF;
 PERFORM arm_issue_blocker_wakeup(ws,target);
 RETURN NULL;
END $$;
CREATE TRIGGER issue_blocker_change AFTER INSERT OR UPDATE OR DELETE ON issue_blocker
 FOR EACH ROW EXECUTE FUNCTION record_issue_blocker_change();

CREATE FUNCTION record_issue_stage_blockers() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER issue_stage_blockers AFTER INSERT OR UPDATE OF status,parent_issue_id,stage ON issue
 FOR EACH ROW EXECUTE FUNCTION record_issue_stage_blockers();

CREATE FUNCTION record_deleted_child_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE candidate uuid;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM issue WHERE id=OLD.parent_issue_id AND workspace_id=OLD.workspace_id) THEN RETURN NULL; END IF;
 INSERT INTO issue_child_event(workspace_id,parent_id,child_id,kind,source_task_id)
 VALUES(OLD.workspace_id,OLD.parent_issue_id,OLD.id,'detached',NULLIF(current_setting('multica.source_task_id',true),'')::uuid);
 FOR candidate IN SELECT id FROM issue WHERE workspace_id=OLD.workspace_id AND parent_issue_id=OLD.parent_issue_id AND stage IS NOT NULL
 LOOP PERFORM arm_issue_blocker_wakeup(OLD.workspace_id,candidate); END LOOP;
 RETURN NULL;
END $$;
CREATE TRIGGER issue_child_event_delete AFTER DELETE ON issue FOR EACH ROW
 WHEN (OLD.parent_issue_id IS NOT NULL) EXECUTE FUNCTION record_deleted_child_event();
