DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE system_rule IS NOT NULL);
DELETE FROM issue_wakeup WHERE system_rule IS NOT NULL;

CREATE OR REPLACE FUNCTION guard_issue_wakeup_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.enabled THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND OLD.enabled AND OLD.issue_id=NEW.issue_id AND OLD.workspace_id=NEW.workspace_id THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('issue-wakeup-capacity:'||NEW.workspace_id::text,0));
 IF (SELECT count(*) FROM (SELECT 1 FROM issue_wakeup WHERE workspace_id=NEW.workspace_id AND issue_id=NEW.issue_id AND enabled AND id<>NEW.id LIMIT 32) slots)>=32 THEN
  RAISE EXCEPTION 'An issue can have at most 32 enabled wakeups' USING ERRCODE='23514',CONSTRAINT='issue_wakeup_active_limit';
 END IF;
 IF (SELECT count(*) FROM (SELECT 1 FROM issue_wakeup WHERE workspace_id=NEW.workspace_id AND enabled AND id<>NEW.id LIMIT 1000) slots)>=1000 THEN
  RAISE EXCEPTION 'A workspace can have at most 1000 enabled wakeups' USING ERRCODE='23514',CONSTRAINT='issue_wakeup_active_limit';
 END IF;
 RETURN NEW;
END $$;

ALTER TABLE issue_wakeup
 ALTER COLUMN created_by SET NOT NULL,
 ALTER COLUMN agent_id SET NOT NULL,
 DROP COLUMN IF EXISTS customized_at,
 DROP COLUMN IF EXISTS system_rule;
