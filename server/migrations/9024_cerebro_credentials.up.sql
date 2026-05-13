-- JEH-1196 — Central credential registry. Stores every underlying secret
-- Multica needs to govern (repo deploy keys, MCP bearer tokens, API keys,
-- GCP service-account credentials, webhook signing secrets, SSO/SAML certs,
-- OAuth tokens, object-storage credentials) so policy, rotation, and audit
-- can hang off a single source of truth instead of ad-hoc per-resource
-- columns.
--
-- The encrypted plaintext lives in encrypted_value (BYTEA, AES-256-GCM —
-- nonce || ciphertext || tag). The Go service is the only thing that
-- handles plaintext; the database never sees it. value_hint stores a
-- non-sensitive masked preview (e.g. "sk-***abcd") for list views.
--
-- credential_bindings ties a credential to the ressource that uses it
-- (workspace, project, agent, etc.) so a single secret can back several
-- resources and lifecycle policy (rotate, revoke) cascades to all of them.

CREATE TABLE IF NOT EXISTS cerebro_credential (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    type             TEXT NOT NULL CHECK (type IN (
        'repo_deploy_key',
        'mcp_bearer',
        'api_key',
        'gcp_credentials',
        'webhook_secret',
        'sso_cert',
        'oauth_token',
        'object_storage_key'
    )),
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    encrypted_value  BYTEA NOT NULL,
    value_hint       TEXT NOT NULL DEFAULT '',
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_type  TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id    UUID NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ,
    last_rotated_at  TIMESTAMPTZ,
    CONSTRAINT cerebro_credential_name_unique
        UNIQUE (workspace_id, type, name)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_credential_workspace
    ON cerebro_credential (workspace_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_credential_expires_at
    ON cerebro_credential (expires_at)
    WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS cerebro_credential_binding (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_id   UUID NOT NULL REFERENCES cerebro_credential(id) ON DELETE CASCADE,
    resource_type   TEXT NOT NULL,
    resource_id     UUID NOT NULL,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id   UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_credential_binding_unique
        UNIQUE (credential_id, resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_credential_binding_credential
    ON cerebro_credential_binding (credential_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_credential_binding_resource
    ON cerebro_credential_binding (resource_type, resource_id);

-- Audit-log table for every privileged action on a credential (create,
-- update, delete, reveal, rotate, bind, unbind). reveal is the load-bearing
-- one — JEH-1196 requires that plaintext disclosure leaves a trail.
CREATE TABLE IF NOT EXISTS cerebro_credential_audit (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    credential_id   UUID NOT NULL,
    action          TEXT NOT NULL CHECK (action IN (
        'create', 'update', 'delete', 'reveal', 'rotate', 'bind', 'unbind'
    )),
    actor_type      TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id        UUID,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_credential_audit_credential
    ON cerebro_credential_audit (credential_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_cerebro_credential_audit_workspace
    ON cerebro_credential_audit (workspace_id, created_at DESC);
