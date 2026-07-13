-- FIR-3172: versioned mini-app catalog, grants, storage, workflows, and audit.
-- Registry data access is deliberately not proxied through these tables.

CREATE TABLE IF NOT EXISTS cerebro_app (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    icon TEXT NOT NULL DEFAULT 'blocks',
    folder TEXT NOT NULL DEFAULT '',
    owner_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    approver_ids UUID[] NOT NULL DEFAULT '{}',
    current_version TEXT,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'published', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_workspace ON cerebro_app(workspace_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_app_owner ON cerebro_app(owner_id);

CREATE TABLE IF NOT EXISTS cerebro_app_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES cerebro_app(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    content_snapshot JSONB NOT NULL,
    release_notes TEXT NOT NULL CHECK (length(btrim(release_notes)) > 0),
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (app_id, version)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_version_app
    ON cerebro_app_version(app_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_app_change_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES cerebro_app(id) ON DELETE CASCADE,
    base_version TEXT,
    proposed_version TEXT NOT NULL,
    proposed_snapshot JSONB NOT NULL,
    release_notes TEXT NOT NULL CHECK (length(btrim(release_notes)) > 0),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'merged')),
    proposed_by UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    reviewed_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    review_comment TEXT NOT NULL DEFAULT '',
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_change_request_pending
    ON cerebro_app_change_request(app_id, created_at DESC)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS cerebro_app_grant (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES cerebro_app(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    registry_profile_ref TEXT,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'revoked')),
    requested_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    approved_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (app_id, version)
);

CREATE TABLE IF NOT EXISTS cerebro_app_kv (
    app_id UUID NOT NULL REFERENCES cerebro_app(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value JSONB NOT NULL,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (app_id, key)
);

CREATE TABLE IF NOT EXISTS cerebro_app_workflow_def (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    app_id UUID REFERENCES cerebro_app(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    definition JSONB NOT NULL,
    version TEXT NOT NULL DEFAULT '1.0.0',
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    owner_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_workflow_def_workspace
    ON cerebro_app_workflow_def(workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_app_workflow_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES cerebro_app_workflow_def(id) ON DELETE CASCADE,
    workflow_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'waiting', 'succeeded', 'failed', 'cancelled')),
    identity_envelope JSONB NOT NULL,
    trigger_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    step_log JSONB NOT NULL DEFAULT '[]'::jsonb,
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_workflow_run_workflow
    ON cerebro_app_workflow_run(workflow_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_app_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    app_id UUID REFERENCES cerebro_app(id) ON DELETE SET NULL,
    workflow_id UUID REFERENCES cerebro_app_workflow_def(id) ON DELETE SET NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'agent', 'app', 'system')),
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_audit_workspace
    ON cerebro_app_audit_log(workspace_id, created_at DESC);
