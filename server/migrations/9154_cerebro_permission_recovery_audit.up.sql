CREATE TABLE IF NOT EXISTS cerebro_permission_recovery_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    source_fingerprint TEXT NOT NULL,
    approval_id UUID NOT NULL REFERENCES cerebro_approval_request(id),
    approved_by UUID NOT NULL REFERENCES "user"(id),
    imported_count INTEGER NOT NULL CHECK (imported_count >= 0),
    already_present_count INTEGER NOT NULL CHECK (already_present_count >= 0),
    imported_identities JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, source_fingerprint)
);
