-- Service tokens: non-personal, workspace-bound, scoped, revocable API
-- credentials for external systems and provisioned agents. FIR-3608.
--
-- Modeled on the existing machine tokens (108_task_token, 029_daemon_token)
-- but requestable and long-lived like a PAT. The key difference from a
-- personal_access_token (011): bound to a WORKSPACE, not a user, and it
-- carries an explicit scope set so it can be least-privilege ("skills:read")
-- instead of inheriting a whole human's rights. Raw token is "msv_" + 40 hex;
-- only its hex SHA-256 hash is stored, exactly like the PAT/daemon tables.

CREATE TABLE IF NOT EXISTS cerebro_service_token (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    -- Scope grants as a JSON array of "host:action" strings
    -- (e.g. ["skills:read","agents:read"]). host:action is chosen to line up
    -- with the tool-policy HostOf/ActionOf model so these scopes stay
    -- forward-compatible with a future tool-policy-native resolver.
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked BOOLEAN NOT NULL DEFAULT FALSE,
    -- The human owner/admin who minted the token. Kept for audit only; the
    -- token never resolves to this user's identity or rights.
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_service_token_hash ON cerebro_service_token(token_hash);
CREATE INDEX IF NOT EXISTS idx_cerebro_service_token_workspace ON cerebro_service_token(workspace_id, revoked);

-- Audit trail for issuance / revocation / use, closing the API-access audit
-- gap (a PAT records only last_used_at). One row per lifecycle event.
CREATE TABLE IF NOT EXISTS cerebro_service_token_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_token_id UUID NOT NULL REFERENCES cerebro_service_token(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- 'issued' | 'revoked' | 'used'
    event TEXT NOT NULL,
    -- The human who issued/revoked, when the event was human-driven. NULL for
    -- 'used' events (a machine call has no human actor).
    actor_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    detail JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_service_token_audit_token ON cerebro_service_token_audit(service_token_id, created_at);
CREATE INDEX IF NOT EXISTS idx_cerebro_service_token_audit_workspace ON cerebro_service_token_audit(workspace_id, created_at);
