-- Cerebro approval inbox (JEH-2131, FIR-2131 — phase 3 of the core
-- permissions stack).
--
-- When the permission resolver (phase 2) returns `needs_approval` for an
-- (actor, capability, resource) request, the enforcement gate materialises the
-- pending ask as a row here. A human then approves, rejects or delegates it
-- from the approval inbox; every decision lands in the append-only audit
-- ledger below.
--
-- cerebro_approval_request — one row per pending/decided ask.
-- cerebro_approval_audit   — append-only ledger; one row per lifecycle event
--   (created / approved / rejected / delegated / expired / cancelled).
--
-- Idempotent: uses IF NOT EXISTS so re-running up after a down is a no-op.

CREATE TABLE IF NOT EXISTS cerebro_approval_request (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- Who asked. requester_id has no FK so it can hold either a user id
    -- (member) or an agent id, mirroring the grant audit actor columns.
    requester_type TEXT NOT NULL,
    requester_id   UUID NOT NULL,
    CONSTRAINT cerebro_approval_request_requester_type_check CHECK (
        requester_type IN ('member', 'agent')
    ),

    -- The agent the actor was running through (NULL for direct member calls).
    agent_id UUID,

    -- What was requested. Mirrors permissions.Request.
    capability        TEXT NOT NULL,
    resource          TEXT NOT NULL DEFAULT '',
    reason            TEXT NOT NULL DEFAULT '',
    matched_grant_ids JSONB NOT NULL DEFAULT '[]',
    context           JSONB NOT NULL DEFAULT '{}',

    -- Lifecycle.
    status TEXT NOT NULL DEFAULT 'pending',
    CONSTRAINT cerebro_approval_request_status_check CHECK (
        status IN ('pending', 'approved', 'rejected', 'delegated', 'expired', 'cancelled')
    ),

    -- Decision (NULL until decided).
    decided_by_id UUID,
    decided_at    TIMESTAMPTZ,
    decision_note TEXT,

    -- Delegation target (set when status = 'delegated').
    delegated_to_type TEXT,
    delegated_to_id   UUID,
    CONSTRAINT cerebro_approval_request_delegated_to_type_check CHECK (
        delegated_to_type IS NULL OR delegated_to_type IN ('member', 'group')
    ),

    -- Timeout. NULL = never expires.
    expires_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_approval_request_workspace_status
    ON cerebro_approval_request (workspace_id, status);

CREATE INDEX IF NOT EXISTS idx_cerebro_approval_request_workspace_created
    ON cerebro_approval_request (workspace_id, created_at DESC);

-- Pending asks past their deadline are swept to 'expired'; this partial index
-- keeps that sweep cheap.
CREATE INDEX IF NOT EXISTS idx_cerebro_approval_request_pending_expiry
    ON cerebro_approval_request (expires_at)
    WHERE status = 'pending' AND expires_at IS NOT NULL;

-- Audit ledger — append-only; approval_id is nullable so audit rows survive a
-- cascade delete of the parent request.
CREATE TABLE IF NOT EXISTS cerebro_approval_audit (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    approval_id  UUID REFERENCES cerebro_approval_request(id) ON DELETE SET NULL,
    action       TEXT NOT NULL,
    CONSTRAINT cerebro_approval_audit_action_check CHECK (
        action IN ('created', 'approved', 'rejected', 'delegated', 'expired', 'cancelled')
    ),
    actor_type TEXT,
    actor_id   UUID,
    surface    TEXT NOT NULL DEFAULT 'api',
    CONSTRAINT cerebro_approval_audit_surface_check CHECK (
        surface IN ('api', 'cli', 'ui', 'mcp', 'system')
    ),
    note       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_approval_audit_approval
    ON cerebro_approval_audit (approval_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_approval_audit_workspace
    ON cerebro_approval_audit (workspace_id, created_at DESC);
