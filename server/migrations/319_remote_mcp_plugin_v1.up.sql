-- Remote MCP Plugin V1 adds revisioned installation configuration and a
-- dedicated encrypted credential store. Contribution declarations remain in
-- immutable Plugin releases; endpoint, grants, and credentials are owned by a
-- workspace installation and never enter an artifact or execution manifest.

ALTER TABLE plugin_contribution
    DROP CONSTRAINT plugin_contribution_type_check;

ALTER TABLE plugin_contribution
    ADD CONSTRAINT plugin_contribution_type_check
    CHECK (type IN ('agent.skill.v1', 'tool.remote-mcp.v1'));

CREATE TABLE plugin_remote_mcp_secret (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    contribution_id UUID NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) > 0),
    hint TEXT NOT NULL DEFAULT '' CHECK (char_length(hint) <= 32),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    CHECK (
        (status = 'active' AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL)
    )
);

CREATE TABLE plugin_installation_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    contribution_id UUID NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    endpoint TEXT NOT NULL CHECK (char_length(endpoint) BETWEEN 9 AND 2048),
    public_config JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(public_config) = 'object'),
    auth_type TEXT NOT NULL CHECK (auth_type IN ('none', 'bearer', 'header')),
    auth_header TEXT NOT NULL DEFAULT '' CHECK (char_length(auth_header) <= 128),
    secret_ref UUID,
    approved_tools JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(approved_tools) = 'array'),
    schema_digest TEXT,
    failure_policy TEXT NOT NULL DEFAULT 'required' CHECK (failure_policy IN ('required', 'optional')),
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((auth_type = 'none') = (secret_ref IS NULL)),
    CHECK (auth_type <> 'header' OR char_length(auth_header) > 0),
    CHECK ((jsonb_array_length(approved_tools) = 0) = (schema_digest IS NULL)),
    CHECK (schema_digest IS NULL OR schema_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK ((reviewed_by IS NULL) = (reviewed_at IS NULL))
);

CREATE TRIGGER trg_plugin_installation_config_append_only
BEFORE UPDATE ON plugin_installation_config
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();
