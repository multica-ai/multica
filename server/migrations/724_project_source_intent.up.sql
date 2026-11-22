-- Existing projects have not expressed an intent. Do not infer no-code from an empty list.
ALTER TABLE project ADD COLUMN source_intent text NOT NULL DEFAULT 'unspecified'
    CHECK (source_intent IN ('unspecified', 'resources', 'no_code'));
ALTER TABLE agent_task_queue ADD COLUMN execution_source jsonb;

-- Serialize an explicit no-code choice with resource edits on the project row.
CREATE FUNCTION check_project_source_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.source_intent = 'no_code' AND EXISTS (SELECT 1 FROM project_resource WHERE project_id = NEW.id) THEN
        RAISE EXCEPTION 'Remove project resources before marking the project no code' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER project_source_intent_check BEFORE UPDATE OF source_intent ON project
FOR EACH ROW EXECUTE FUNCTION check_project_source_intent();

CREATE FUNCTION sync_project_source_intent() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN target := OLD.project_id; ELSE target := NEW.project_id; END IF;
    PERFORM id FROM project WHERE id = target FOR UPDATE;
    UPDATE project SET source_intent = CASE
        WHEN EXISTS (SELECT 1 FROM project_resource WHERE project_id = target) THEN 'resources'
        WHEN source_intent = 'resources' THEN 'unspecified'
        ELSE source_intent END
    WHERE id = target;
    RETURN NULL;
END;
$$;
CREATE TRIGGER project_resource_source_intent AFTER INSERT OR DELETE ON project_resource
FOR EACH ROW EXECUTE FUNCTION sync_project_source_intent();
