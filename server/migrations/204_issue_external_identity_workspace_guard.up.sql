-- Forward-repair migration 203 for databases that applied an earlier
-- workspace-composite foreign key. Keep issue(id) as the sole referenced
-- key and enforce workspace agreement with migration-owned triggers.
LOCK TABLE issue, issue_external_identity IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM issue_external_identity e
        JOIN issue i ON i.id = e.issue_id
        WHERE i.workspace_id IS DISTINCT FROM e.workspace_id
    ) THEN
        RAISE EXCEPTION 'cannot repair external identities with mismatched issue workspaces'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

ALTER TABLE issue_external_identity
    DROP CONSTRAINT IF EXISTS issue_external_identity_issue_fk;

ALTER TABLE issue_external_identity
    ADD CONSTRAINT issue_external_identity_issue_fk
    FOREIGN KEY (issue_id) REFERENCES issue(id) ON DELETE CASCADE NOT VALID;

ALTER TABLE issue_external_identity
    VALIDATE CONSTRAINT issue_external_identity_issue_fk;

-- This broad unique constraint was introduced solely to support the old
-- composite foreign key. It is not part of the issue table's domain model.
ALTER TABLE issue
    DROP CONSTRAINT IF EXISTS uq_issue_workspace_id;

CREATE OR REPLACE FUNCTION issue_external_identity_enforce_workspace_180()
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

DROP TRIGGER IF EXISTS issue_external_identity_workspace_180 ON issue_external_identity;
CREATE TRIGGER issue_external_identity_workspace_180
BEFORE INSERT OR UPDATE OF workspace_id, issue_id ON issue_external_identity
FOR EACH ROW EXECUTE FUNCTION issue_external_identity_enforce_workspace_180();

CREATE OR REPLACE FUNCTION issue_external_identity_guard_issue_workspace_180()
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

DROP TRIGGER IF EXISTS issue_external_identity_issue_workspace_180 ON issue;
CREATE TRIGGER issue_external_identity_issue_workspace_180
BEFORE UPDATE OF workspace_id ON issue
FOR EACH ROW EXECUTE FUNCTION issue_external_identity_guard_issue_workspace_180();
