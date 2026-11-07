-- Platform-owned wakeup rules are ordinary issue_wakeup rows. system_rule names
-- the rule ('child_done': wake the issue's assignee when a stage of its
-- sub-issues finishes). Such a row has no fixed agent or creator: it targets
-- the issue's assignee at the moment it fires, so agent_id and created_by stay
-- NULL for it. customized_at records that a person changed the rule on this
-- issue; until then it follows the workspace default. All changes are
-- additive for existing rows.
ALTER TABLE issue_wakeup
 ADD COLUMN IF NOT EXISTS system_rule text CHECK (system_rule IN ('child_done')),
 ADD COLUMN IF NOT EXISTS customized_at timestamptz,
 ALTER COLUMN agent_id DROP NOT NULL,
 ALTER COLUMN created_by DROP NOT NULL;

-- System rules exist once per parent issue and do not use the per-issue or
-- per-workspace capacity people and agents share.
CREATE OR REPLACE FUNCTION guard_issue_wakeup_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.enabled OR NEW.system_rule IS NOT NULL THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND OLD.enabled AND OLD.issue_id=NEW.issue_id AND OLD.workspace_id=NEW.workspace_id THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('issue-wakeup-capacity:'||NEW.workspace_id::text,0));
 IF (SELECT count(*) FROM (SELECT 1 FROM issue_wakeup WHERE workspace_id=NEW.workspace_id AND issue_id=NEW.issue_id AND enabled AND system_rule IS NULL AND id<>NEW.id LIMIT 32) slots)>=32 THEN
  RAISE EXCEPTION 'An issue can have at most 32 enabled wakeups' USING ERRCODE='23514',CONSTRAINT='issue_wakeup_active_limit';
 END IF;
 IF (SELECT count(*) FROM (SELECT 1 FROM issue_wakeup WHERE workspace_id=NEW.workspace_id AND enabled AND system_rule IS NULL AND id<>NEW.id LIMIT 1000) slots)>=1000 THEN
  RAISE EXCEPTION 'A workspace can have at most 1000 enabled wakeups' USING ERRCODE='23514',CONSTRAINT='issue_wakeup_active_limit';
 END IF;
 RETURN NEW;
END $$;
