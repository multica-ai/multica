-- A real revision for existing and newly created entries. The trigger also
-- versions writes from older servers during a rolling deployment.
ALTER TABLE workspace_mcp_server
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0);

CREATE FUNCTION advance_workspace_mcp_revision() RETURNS trigger AS $$
BEGIN
    NEW.revision := OLD.revision + 1;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER workspace_mcp_revision_before_update
    BEFORE UPDATE ON workspace_mcp_server
    FOR EACH ROW EXECUTE FUNCTION advance_workspace_mcp_revision();
