-- All API, plugin, integration and internal writers meet this final guard.
-- Only the controller's transaction sets the local writer flag. That flag is
-- never accepted from HTTP headers and is automatically cleared at transaction end.
CREATE FUNCTION enforce_issue_controller() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cfg jsonb; k text;
BEGIN
    SELECT config INTO cfg FROM issue_controller
      WHERE workspace_id = OLD.workspace_id AND issue_id = OLD.id;
    IF NOT FOUND OR current_setting('multica.controller_writer', true) = '1' THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'issue projection is controller owned' USING ERRCODE='42501';
    END IF;
    IF NEW.status IS DISTINCT FROM OLD.status OR NEW.assignee_id IS DISTINCT FROM OLD.assignee_id
       OR NEW.assignee_type IS DISTINCT FROM OLD.assignee_type OR NEW.project_id IS DISTINCT FROM OLD.project_id THEN
        RAISE EXCEPTION 'issue projection is controller owned' USING ERRCODE='42501';
    END IF;
    FOREACH k IN ARRAY ARRAY['controller_revision','controller_checked_at','controller_state','last_progress_what','last_progress_at','owner_action','delivery_state'] LOOP
        IF NEW.metadata->k IS DISTINCT FROM OLD.metadata->k THEN
            RAISE EXCEPTION 'issue metadata is controller owned' USING ERRCODE='42501';
        END IF;
    END LOOP;
    FOR k IN SELECT jsonb_array_elements_text(COALESCE(cfg->'protected_property_ids','[]')) LOOP
        IF NEW.properties->k IS DISTINCT FROM OLD.properties->k THEN
            RAISE EXCEPTION 'issue property is controller owned' USING ERRCODE='42501';
        END IF;
    END LOOP;
    RETURN NEW;
END $$;
CREATE TRIGGER issue_controller_guard BEFORE UPDATE OR DELETE ON issue
    FOR EACH ROW EXECUTE FUNCTION enforce_issue_controller();

-- No automatic status/mention/squad trigger may create a provider attempt for
-- the controlled cohort. Its exact authority must exist before task insertion.
CREATE FUNCTION enforce_controlled_launch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM issue_controller WHERE issue_id=NEW.issue_id)
       AND NOT EXISTS (SELECT 1 FROM controlled_run r WHERE r.run_id=NEW.id
           AND r.issue_id=NEW.issue_id AND r.manifest->>'profile_id'=NEW.agent_id::text
           AND r.manifest->>'runtime_id'=NEW.runtime_id::text) THEN
        RAISE EXCEPTION 'run requires controller launch authority' USING ERRCODE='42501';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER controlled_launch_guard BEFORE INSERT ON agent_task_queue
    FOR EACH ROW EXECUTE FUNCTION enforce_controlled_launch();
