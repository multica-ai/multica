-- Integration credentials/connections remain Workspace-owned. This table is
-- the explicit authorization/reference layer for where each connection may be
-- used; binding never transfers ownership or copies a secret into a Space.
CREATE TABLE integration_space_binding (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    connection_id UUID NOT NULL,
    space_id UUID NOT NULL,
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, connection_id, space_id),
    FOREIGN KEY (workspace_id, space_id)
        REFERENCES workspace_space(workspace_id, id) ON DELETE CASCADE,
    CHECK (provider IN ('github', 'slack', 'feishu'))
);

CREATE INDEX idx_integration_space_binding_space
    ON integration_space_binding(workspace_id, space_id, provider);

-- Preserve current behavior for existing installations: every existing
-- Workspace connection starts bound to each current active Space. New
-- connections are intentionally unbound until an admin chooses their Spaces.
INSERT INTO integration_space_binding (workspace_id, provider, connection_id, space_id, created_by)
SELECT gi.workspace_id, 'github', gi.id, ws.id, gi.connected_by_id
FROM github_installation gi
JOIN workspace_space ws ON ws.workspace_id = gi.workspace_id AND ws.archived_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO integration_space_binding (workspace_id, provider, connection_id, space_id, created_by)
SELECT ci.workspace_id, ci.channel_type, ci.id, ws.id, ci.installer_user_id
FROM channel_installation ci
JOIN workspace_space ws ON ws.workspace_id = ci.workspace_id AND ws.archived_at IS NULL
WHERE ci.channel_type IN ('slack', 'feishu')
  AND ci.status = 'active'
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION cleanup_integration_space_binding()
RETURNS trigger AS $$
BEGIN
    DELETE FROM integration_space_binding
    WHERE provider = TG_ARGV[0] AND connection_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_github_integration_binding_cleanup
AFTER DELETE ON github_installation
FOR EACH ROW EXECUTE FUNCTION cleanup_integration_space_binding('github');

CREATE OR REPLACE FUNCTION cleanup_channel_integration_space_binding()
RETURNS trigger AS $$
BEGIN
    DELETE FROM integration_space_binding
    WHERE provider = OLD.channel_type AND connection_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_channel_integration_binding_cleanup
AFTER DELETE ON channel_installation
FOR EACH ROW EXECUTE FUNCTION cleanup_channel_integration_space_binding();
