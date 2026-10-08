-- Extend transactional capture without an event archive or a second run lifecycle.
-- Payloads contain references and changed field names, never comment bodies,
-- attachment URLs, or arbitrary metadata values.
CREATE OR REPLACE FUNCTION capture_issue_wakeup(p_issue uuid, p_type text, p_key text, p_agent uuid, p_task uuid, p_payload jsonb)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE w issue_wakeup; owner_workspace uuid; evidence jsonb;
BEGIN
 IF NOT EXISTS (SELECT 1 FROM issue_wakeup WHERE issue_id=p_issue AND enabled AND kind='event' AND p_type=ANY(event_types)) THEN RETURN; END IF;
 IF p_agent IS NULL AND current_setting('multica.actor_type',true)='agent' THEN
  p_agent := NULLIF(current_setting('multica.actor_id',true),'')::uuid;
 END IF;
 SELECT i.workspace_id INTO owner_workspace FROM issue i
 WHERE i.id=p_issue AND i.status NOT IN ('done','cancelled') AND NOT EXISTS
 (SELECT 1 FROM issue_status s WHERE s.workspace_id=i.workspace_id AND s.key=i.status AND s.category IN ('done','closed'));
 IF owner_workspace IS NULL THEN RETURN; END IF;
 evidence := jsonb_build_object('event_id',p_key,'event_type',p_type,'version',1,
  'occurred_at',clock_timestamp(),'workspace_id',owner_workspace,'issue_id',p_issue,
  'source_task_id',p_task,'agent_id',p_agent,
  'actor_type',COALESCE(NULLIF(current_setting('multica.actor_type',true),''),CASE WHEN p_agent IS NOT NULL THEN 'agent' ELSE 'system' END),
  'actor_id',COALESCE(NULLIF(current_setting('multica.actor_id',true),''),p_agent::text)) || p_payload;
 FOR w IN SELECT * FROM issue_wakeup WHERE issue_id=p_issue AND workspace_id=owner_workspace AND enabled AND kind='event'
   AND p_type=ANY(event_types) AND (filter_agent_id IS NULL OR filter_agent_id=p_agent)
   AND (filter_task_id IS NULL OR filter_task_id=p_task)
   AND NOT EXISTS (SELECT 1 FROM agent_task_queue t WHERE t.id=p_task AND t.context->>'wakeup_id'=issue_wakeup.id::text)
   ORDER BY id
 LOOP
  INSERT INTO issue_wakeup_receipt(id,wakeup_id,revision,event_key,event_type,payload)
   VALUES(gen_random_uuid(),w.id,w.revision,p_key,p_type,evidence) ON CONFLICT DO NOTHING;
 END LOOP;
END $$;

CREATE OR REPLACE FUNCTION capture_task_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE event_type text; event_key text; old_status text;
BEGIN
 IF NEW.issue_id IS NULL THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW.status IS NOT DISTINCT FROM OLD.status THEN RETURN NEW; END IF;
  old_status := OLD.status;
 END IF;
 event_type := CASE NEW.status WHEN 'running' THEN 'task.started' ELSE 'task.'||NEW.status END;
 IF NEW.status NOT IN ('queued','dispatched','running','deferred','waiting_local_directory','completed','failed','cancelled') THEN RETURN NEW; END IF;
 -- Terminal keys also match the locked registration check. Repeated queue /
 -- defer transitions are separate facts and must not share a lifetime key.
 event_key := CASE WHEN NEW.status IN ('completed','failed','cancelled') THEN NEW.id::text||':'||NEW.status ELSE gen_random_uuid()::text END;
 PERFORM capture_issue_wakeup(NEW.issue_id,event_type,event_key,NEW.agent_id,NEW.id,
  jsonb_build_object('task_id',NEW.id,'agent_id',NEW.agent_id,'status',NEW.status,'previous_status',old_status,
   'retry_of_task_id',NEW.retry_of_task_id,'rerun_of_task_id',NEW.rerun_of_task_id));
 RETURN NEW;
END $$;
DROP TRIGGER capture_task_wakeup ON agent_task_queue;
CREATE TRIGGER capture_task_wakeup AFTER INSERT OR UPDATE OF status ON agent_task_queue FOR EACH ROW EXECUTE FUNCTION capture_task_wakeup();

CREATE OR REPLACE FUNCTION capture_comment_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c comment; event_type text; source_id uuid; source_agent uuid; payload jsonb; root_id uuid;
BEGIN
 c := CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
 IF NOT EXISTS (SELECT 1 FROM issue_wakeup WHERE issue_id=c.issue_id AND enabled AND kind='event') THEN RETURN c; END IF;
 source_id := NULLIF(current_setting('multica.source_task_id',true),'')::uuid;
 IF TG_OP='INSERT' THEN
  event_type := 'comment.created';
  source_id := COALESCE(source_id,c.source_task_id);
  source_agent := CASE WHEN c.author_type='agent' THEN c.author_id END;
 ELSIF TG_OP='DELETE' THEN
  -- Pruning an existing tombstone is storage cleanup, not another deletion.
  IF OLD.deleted_at IS NOT NULL THEN RETURN OLD; END IF;
  event_type := 'comment.deleted';
 ELSIF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
  event_type := 'comment.deleted';
 ELSIF NEW.deleted_at IS NOT NULL THEN RETURN NEW;
 ELSIF OLD.resolved_at IS NULL AND NEW.resolved_at IS NOT NULL THEN event_type := 'comment.resolved';
 ELSIF OLD.resolved_at IS NOT NULL AND NEW.resolved_at IS NULL THEN event_type := 'comment.unresolved';
 ELSIF NEW.content IS DISTINCT FROM OLD.content THEN event_type := 'comment.updated';
 ELSE RETURN NEW;
 END IF;
 IF source_id IS NOT NULL THEN SELECT agent_id INTO source_agent FROM agent_task_queue WHERE id=source_id; END IF;
 -- Keep author and actor distinct: an admin editing an agent's comment is
 -- not an event produced by that agent's original run.
 IF TG_OP<>'INSERT' AND source_id IS NULL THEN
  source_agent := CASE WHEN current_setting('multica.actor_type',true)='agent' THEN NULLIF(current_setting('multica.actor_id',true),'')::uuid END;
 END IF;
 root_id := c.id;
 IF c.parent_id IS NOT NULL THEN
  WITH RECURSIVE ancestors AS (
   SELECT id,parent_id,1 AS depth FROM comment WHERE id=c.parent_id AND issue_id=c.issue_id AND workspace_id=c.workspace_id
   UNION ALL SELECT p.id,p.parent_id,a.depth+1 FROM comment p JOIN ancestors a ON p.id=a.parent_id
    WHERE p.issue_id=c.issue_id AND p.workspace_id=c.workspace_id AND a.depth<256
  ) SELECT id INTO root_id FROM ancestors ORDER BY depth DESC LIMIT 1;
 END IF;
 payload := jsonb_build_object('comment_id',c.id,'parent_comment_id',c.parent_id,'thread_id',COALESCE(root_id,c.id),
  'author_type',c.author_type,'author_id',c.author_id);
 IF TG_OP='INSERT' THEN payload := payload || jsonb_build_object('actor_type',c.author_type,'actor_id',c.author_id); END IF;
 PERFORM capture_issue_wakeup(c.issue_id,event_type,gen_random_uuid()::text,source_agent,source_id,payload);
 RETURN c;
END $$;
DROP TRIGGER capture_comment_wakeup ON comment;
CREATE TRIGGER capture_comment_wakeup AFTER INSERT OR UPDATE OR DELETE ON comment FOR EACH ROW EXECUTE FUNCTION capture_comment_wakeup();

-- Closing still wins over every subscription. Capture of non-terminal changes
-- lives in its own trigger so revision-only writes never produce a new input.
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

CREATE FUNCTION capture_issue_collaboration_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_id uuid; source_agent uuid; fields jsonb; field_name text; event_type text;
 before_row jsonb; after_row jsonb; event_key text;
BEGIN
 -- Avoid serializing potentially large issue documents on the ordinary path.
 IF NOT EXISTS (SELECT 1 FROM issue_wakeup WHERE issue_id=NEW.id AND enabled AND kind='event') THEN RETURN NEW; END IF;
 before_row := to_jsonb(OLD); after_row := to_jsonb(NEW); event_key := gen_random_uuid()::text;
 SELECT jsonb_agg(k ORDER BY k) INTO fields FROM unnest(ARRAY[
  'title','description','status','priority','assignee_type','assignee_id','parent_issue_id','project_id',
  'due_date','start_date','stage','properties','metadata','acceptance_criteria','context_refs','triage_state']) k
 WHERE before_row->k IS DISTINCT FROM after_row->k;
 IF fields IS NULL THEN RETURN NEW; END IF;
 source_id := NULLIF(current_setting('multica.source_task_id',true),'')::uuid;
 SELECT agent_id INTO source_agent FROM agent_task_queue WHERE id=source_id;
 IF source_agent IS NULL AND current_setting('multica.actor_type',true)='agent' THEN
  source_agent := NULLIF(current_setting('multica.actor_id',true),'')::uuid;
 END IF;
 PERFORM capture_issue_wakeup(NEW.id,'issue.updated',event_key||':updated',source_agent,source_id,jsonb_build_object('changed_fields',fields));
 FOR field_name,event_type IN SELECT * FROM (VALUES
  ('status','issue.status_changed'),('assignee_id','issue.assignee_changed'),
  ('parent_issue_id','issue.parent_changed'),('project_id','issue.project_changed'),
  ('properties','issue.properties_changed'),('metadata','issue.metadata_changed')) v(f,e)
 LOOP
  IF fields ? field_name OR (field_name='assignee_id' AND fields ? 'assignee_type') THEN
   PERFORM capture_issue_wakeup(NEW.id,event_type,event_key||':'||field_name,source_agent,source_id,
    CASE WHEN field_name IN ('metadata','properties') THEN
     jsonb_build_object('changed_fields',(SELECT jsonb_agg(k ORDER BY k) FROM
      (SELECT jsonb_object_keys(before_row->field_name) k UNION SELECT jsonb_object_keys(after_row->field_name)) keys
      WHERE (before_row->field_name)->k IS DISTINCT FROM (after_row->field_name)->k))
    WHEN field_name='status' THEN jsonb_build_object('previous_status',OLD.status,'status',NEW.status,
     'previous_category',(SELECT category FROM issue_status WHERE workspace_id=OLD.workspace_id AND key=OLD.status),
     'category',(SELECT category FROM issue_status WHERE workspace_id=NEW.workspace_id AND key=NEW.status))
    WHEN field_name='assignee_id' THEN jsonb_build_object('previous_assignee_id',OLD.assignee_id,'assignee_id',NEW.assignee_id,
     'previous_assignee_type',OLD.assignee_type,'assignee_type',NEW.assignee_type)
    ELSE jsonb_build_object('previous_'||field_name,before_row->field_name,field_name,after_row->field_name) END);
  END IF;
 END LOOP;
 RETURN NEW;
END $$;
CREATE TRIGGER capture_issue_collaboration_wakeup AFTER UPDATE ON issue FOR EACH ROW EXECUTE FUNCTION capture_issue_collaboration_wakeup();

CREATE FUNCTION capture_issue_label_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_data jsonb := CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END; source_id uuid; source_agent uuid;
BEGIN
 source_id := NULLIF(current_setting('multica.source_task_id',true),'')::uuid;
 SELECT agent_id INTO source_agent FROM agent_task_queue WHERE id=source_id;
 PERFORM capture_issue_wakeup((row_data->>'issue_id')::uuid,'issue.labels_changed',gen_random_uuid()::text,source_agent,source_id,
  jsonb_build_object('label_id',row_data->'label_id','action',CASE WHEN TG_OP='INSERT' THEN 'added' ELSE 'removed' END));
 RETURN COALESCE(NEW,OLD);
END $$;
CREATE TRIGGER capture_issue_label_wakeup AFTER INSERT OR DELETE ON issue_to_label FOR EACH ROW EXECUTE FUNCTION capture_issue_label_wakeup();

CREATE FUNCTION capture_reaction_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_data jsonb := CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
 owner_issue uuid; source_id uuid; source_agent uuid; target_type text; target_id uuid; payload jsonb;
BEGIN
 IF TG_TABLE_NAME='comment_reaction' THEN
  target_type := 'comment'; target_id := (row_data->>'comment_id')::uuid;
  SELECT issue_id INTO owner_issue FROM comment WHERE id=target_id AND workspace_id=(row_data->>'workspace_id')::uuid;
 ELSE target_type := 'issue'; target_id := (row_data->>'issue_id')::uuid; owner_issue := target_id;
 END IF;
 source_id := NULLIF(current_setting('multica.source_task_id',true),'')::uuid;
 IF source_id IS NOT NULL THEN SELECT agent_id INTO source_agent FROM agent_task_queue WHERE id=source_id;
 ELSIF TG_OP='INSERT' AND row_data->>'actor_type'='agent' THEN source_agent := (row_data->>'actor_id')::uuid;
 END IF;
 payload := jsonb_build_object('reaction_id',row_data->'id','target_type',target_type,'target_id',target_id,'emoji',row_data->'emoji',
  'reaction_actor_type',row_data->'actor_type','reaction_actor_id',row_data->'actor_id');
 IF TG_OP='INSERT' THEN payload := payload || jsonb_build_object('actor_type',row_data->'actor_type','actor_id',row_data->'actor_id'); END IF;
 PERFORM capture_issue_wakeup(owner_issue,CASE WHEN TG_OP='INSERT' THEN 'reaction.added' ELSE 'reaction.removed' END,
  gen_random_uuid()::text,source_agent,source_id,payload);
 RETURN COALESCE(NEW,OLD);
END $$;
CREATE TRIGGER capture_comment_reaction_wakeup AFTER INSERT OR DELETE ON comment_reaction FOR EACH ROW EXECUTE FUNCTION capture_reaction_wakeup();
CREATE TRIGGER capture_issue_reaction_wakeup AFTER INSERT OR DELETE ON issue_reaction FOR EACH ROW EXECUTE FUNCTION capture_reaction_wakeup();

CREATE FUNCTION capture_attachment_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE before_row jsonb; after_row jsonb; row_data jsonb; before_target uuid; after_target uuid;
 target_type text; source_id uuid; source_agent uuid; owner_issue uuid; event_type text;
BEGIN
 IF TG_OP<>'INSERT' THEN before_row:=to_jsonb(OLD); before_target:=COALESCE(OLD.comment_id,OLD.issue_id); END IF;
 IF TG_OP<>'DELETE' THEN after_row:=to_jsonb(NEW); after_target:=COALESCE(NEW.comment_id,NEW.issue_id); END IF;
 IF before_target IS NOT DISTINCT FROM after_target THEN RETURN COALESCE(NEW,OLD); END IF;
 source_id := NULLIF(current_setting('multica.source_task_id',true),'')::uuid;
 SELECT agent_id INTO source_agent FROM agent_task_queue WHERE id=source_id;
 FOR row_data,event_type IN SELECT * FROM (VALUES (before_row,'attachment.detached'),(after_row,'attachment.attached')) v(r,e)
 LOOP
  -- Source-context snapshots and chat attachments are outside this issue contract.
  IF row_data IS NULL OR row_data->>'source_context_id' IS NOT NULL OR row_data->>'chat_session_id' IS NOT NULL THEN CONTINUE; END IF;
  owner_issue := (row_data->>'issue_id')::uuid;
  target_type := CASE WHEN row_data->>'comment_id' IS NOT NULL THEN 'comment' ELSE 'issue' END;
  IF target_type='comment' THEN
   SELECT issue_id INTO owner_issue FROM comment WHERE id=(row_data->>'comment_id')::uuid AND workspace_id=(row_data->>'workspace_id')::uuid;
  END IF;
  PERFORM capture_issue_wakeup(owner_issue,event_type,gen_random_uuid()::text,source_agent,source_id,
   jsonb_build_object('attachment_id',row_data->'id','target_type',target_type,
    'target_id',COALESCE(row_data->>'comment_id',row_data->>'issue_id')));
 END LOOP;
 RETURN COALESCE(NEW,OLD);
END $$;
CREATE TRIGGER capture_attachment_wakeup AFTER INSERT OR UPDATE OF issue_id,comment_id OR DELETE ON attachment FOR EACH ROW EXECUTE FUNCTION capture_attachment_wakeup();
