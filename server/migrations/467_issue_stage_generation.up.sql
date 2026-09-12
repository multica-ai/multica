-- A stage notification must survive ordinary edits while distinguishing a
-- reopened child or a changed stage membership. Keep this clock in the same
-- write as the issue, including status writers outside the HTTP handlers.
ALTER TABLE issue ADD COLUMN stage_generation BIGINT NOT NULL DEFAULT 0;

CREATE FUNCTION advance_issue_stage_generation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.parent_issue_id IS DISTINCT FROM OLD.parent_issue_id
       OR NEW.stage IS DISTINCT FROM OLD.stage
       OR (NEW.status IS DISTINCT FROM OLD.status
           AND issue_effective_status(OLD.workspace_id, OLD.status) IN ('done', 'cancelled')
           AND issue_effective_status(NEW.workspace_id, NEW.status) NOT IN ('done', 'cancelled')) THEN
        NEW.stage_generation := OLD.stage_generation + 1;
    ELSE
        NEW.stage_generation := OLD.stage_generation;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER issue_stage_generation
BEFORE UPDATE OF status, parent_issue_id, stage ON issue
FOR EACH ROW EXECUTE FUNCTION advance_issue_stage_generation();
