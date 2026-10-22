DROP TRIGGER capture_issue_collaboration_wakeup ON issue;
DROP TRIGGER capture_issue_label_wakeup ON issue_to_label;
DROP TRIGGER capture_comment_reaction_wakeup ON comment_reaction;
DROP TRIGGER capture_issue_reaction_wakeup ON issue_reaction;
DROP TRIGGER capture_attachment_wakeup ON attachment;
DROP FUNCTION capture_issue_collaboration_wakeup();
DROP FUNCTION capture_issue_label_wakeup();
DROP FUNCTION capture_reaction_wakeup();
DROP FUNCTION capture_attachment_wakeup();
DROP TRIGGER capture_task_wakeup ON agent_task_queue;
DROP TRIGGER capture_comment_wakeup ON comment;
-- Capture only matching wakeup inputs, in the source write's transaction.
-- This is not an event history: receipts are consumed by the normal task queue.
CREATE OR REPLACE FUNCTION capture_issue_wakeup(p_issue uuid, p_type text, p_key text, p_agent uuid, p_task uuid, p_payload jsonb)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE w issue_wakeup;
BEGIN
 FOR w IN SELECT * FROM issue_wakeup WHERE issue_id=p_issue AND enabled AND kind='event'
   AND p_type=ANY(event_types) AND (filter_agent_id IS NULL OR filter_agent_id=p_agent)
   AND (filter_task_id IS NULL OR filter_task_id=p_task)
   AND NOT EXISTS (SELECT 1 FROM agent_task_queue t WHERE t.id=p_task AND t.context->>'wakeup_id'=issue_wakeup.id::text)
   ORDER BY id
 LOOP
  INSERT INTO issue_wakeup_receipt(id,wakeup_id,revision,event_key,event_type,payload)
   VALUES(gen_random_uuid(),w.id,w.revision,p_key,p_type,p_payload) ON CONFLICT DO NOTHING;
 END LOOP;
END $$;
CREATE OR REPLACE FUNCTION capture_task_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.issue_id IS NOT NULL AND NEW.status IS DISTINCT FROM OLD.status AND NEW.status IN ('completed','failed','cancelled') THEN
  PERFORM capture_issue_wakeup(NEW.issue_id,'task.'||NEW.status,NEW.id::text||':'||NEW.status,NEW.agent_id,NEW.id,
   jsonb_build_object('task_id',NEW.id,'agent_id',NEW.agent_id,'status',NEW.status));
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER capture_task_wakeup AFTER UPDATE OF status ON agent_task_queue FOR EACH ROW EXECUTE FUNCTION capture_task_wakeup();
CREATE OR REPLACE FUNCTION capture_comment_wakeup() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM capture_issue_wakeup(NEW.issue_id,'comment.created',NEW.id::text,
  CASE WHEN NEW.author_type='agent' THEN NEW.author_id ELSE NULL END,NEW.source_task_id,
  jsonb_build_object('comment_id',NEW.id,'author_type',NEW.author_type,'author_id',NEW.author_id,'source_task_id',NEW.source_task_id));
 RETURN NEW;
END $$;
CREATE TRIGGER capture_comment_wakeup AFTER INSERT ON comment FOR EACH ROW EXECUTE FUNCTION capture_comment_wakeup();
CREATE OR REPLACE FUNCTION stop_issue_wakeups() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE closing boolean;
BEGIN
 closing := TG_OP='DELETE';
 IF NOT closing THEN
  closing := NEW.status IN ('done','cancelled') OR EXISTS
   (SELECT 1 FROM issue_status s WHERE s.workspace_id=NEW.workspace_id AND s.key=NEW.status AND s.category IN ('done','closed'));
 END IF;
 IF closing THEN
  UPDATE issue_wakeup SET enabled=false,disabled_at=clock_timestamp(),updated_at=clock_timestamp()
   WHERE issue_id=OLD.id AND disabled_at IS NULL;
  UPDATE agent_task_queue SET status='cancelled',completed_at=now(),error='Issue closed; wakeup disabled'
   WHERE issue_id=OLD.id AND context->>'wakeup_id' IS NOT NULL AND status IN ('queued','deferred') AND started_at IS NULL;
 ELSE
  IF NEW.status IS DISTINCT FROM OLD.status THEN
   PERFORM capture_issue_wakeup(NEW.id,'issue.status_changed',NEW.id::text||':'||NEW.revision::text,
    (SELECT agent_id FROM agent_task_queue WHERE id=NULLIF(current_setting('multica.source_task_id',true),'')::uuid),
    NULLIF(current_setting('multica.source_task_id',true),'')::uuid,
    jsonb_build_object('issue_id',NEW.id,'status',NEW.status));
  END IF;
 END IF;
 RETURN COALESCE(NEW,OLD);
END $$;
