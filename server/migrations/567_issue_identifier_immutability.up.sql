CREATE FUNCTION enforce_issue_identifier_immutability() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' AND COALESCE(NEW.identifier_prefix, '') = '' THEN
        SELECT COALESCE(NULLIF(UPPER(p.issue_prefix), ''), NULLIF(UPPER(w.issue_prefix), ''), 'WS')
        INTO NEW.identifier_prefix
        FROM workspace w
        LEFT JOIN project p ON p.id = NEW.project_id AND p.workspace_id = NEW.workspace_id
        WHERE w.id = NEW.workspace_id;
    ELSIF TG_OP = 'UPDATE' AND (NEW.identifier_prefix IS DISTINCT FROM OLD.identifier_prefix OR NEW.number IS DISTINCT FROM OLD.number) THEN
        RAISE EXCEPTION 'issue identifier is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER issue_identifier_immutable
BEFORE INSERT OR UPDATE OF identifier_prefix, number ON issue
FOR EACH ROW EXECUTE FUNCTION enforce_issue_identifier_immutability();
