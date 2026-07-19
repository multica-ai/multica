CREATE TABLE issue_external_identity (
    workspace_id UUID NOT NULL,
    namespace TEXT NOT NULL,
    external_id TEXT NOT NULL,
    issue_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, namespace, external_id),
    CONSTRAINT issue_external_identity_namespace_check
        CHECK (namespace ~ '^[a-z][a-z0-9_.:-]{0,127}$'),
    CONSTRAINT issue_external_identity_external_id_check
        CHECK (length(external_id) BETWEEN 1 AND 512 AND octet_length(external_id) <= 1024),
    CONSTRAINT issue_external_identity_issue_fk
        FOREIGN KEY (issue_id)
        REFERENCES issue(id)
        ON DELETE CASCADE
);

CREATE INDEX idx_issue_external_identity_workspace_issue
    ON issue_external_identity(workspace_id, issue_id);

CREATE FUNCTION issue_external_identity_enforce_workspace_180()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM issue
        WHERE id = NEW.issue_id AND workspace_id = NEW.workspace_id
        FOR SHARE
    ) THEN
        RAISE EXCEPTION 'external identity issue must belong to workspace'
            USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER issue_external_identity_workspace_180
BEFORE INSERT OR UPDATE OF workspace_id, issue_id ON issue_external_identity
FOR EACH ROW EXECUTE FUNCTION issue_external_identity_enforce_workspace_180();

CREATE FUNCTION issue_external_identity_guard_issue_workspace_180()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id AND EXISTS (
        SELECT 1 FROM issue_external_identity
        WHERE issue_id = OLD.id
    ) THEN
        RAISE EXCEPTION 'external identity issue workspace cannot change'
            USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER issue_external_identity_issue_workspace_180
BEFORE UPDATE OF workspace_id ON issue
FOR EACH ROW EXECUTE FUNCTION issue_external_identity_guard_issue_workspace_180();
