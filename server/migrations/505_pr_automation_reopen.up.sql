-- Cover every issue writer, including batch updates and older clients. A
-- human/agent reopening a completed task must not immediately be undone by PRs.
CREATE FUNCTION pr_automation_on_issue_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IS DISTINCT FROM OLD.status
       AND issue_effective_status(OLD.workspace_id, OLD.status) = 'done'
       AND issue_effective_status(NEW.workspace_id, NEW.status) NOT IN ('done', 'cancelled')
       AND EXISTS (SELECT 1 FROM pr_automation_policy WHERE workspace_id = NEW.workspace_id) THEN
        INSERT INTO pr_automation_issue (issue_id, workspace_id, disabled)
        VALUES (NEW.id, NEW.workspace_id, TRUE)
        ON CONFLICT (issue_id) DO UPDATE SET disabled = TRUE;
        INSERT INTO activity_log(workspace_id,issue_id,actor_type,action,details)
        VALUES(NEW.workspace_id,NEW.id,'system','pr_automation_updated','{"disabled":true,"reason":"reopened"}'::jsonb);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pr_automation_issue_reopen BEFORE UPDATE OF status ON issue
FOR EACH ROW EXECUTE FUNCTION pr_automation_on_issue_change();
