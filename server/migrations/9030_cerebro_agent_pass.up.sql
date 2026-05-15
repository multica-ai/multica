-- Cerebro agent-pass (JEH-1327).
--
-- An agent-pass is an explicit, machine-readable mandate that "agent A may
-- act on issue I (or its subtree) within scope S, up to spend ceiling C,
-- until expiresAt T". The pre-run gate (server/internal/cerebro/agentpass)
-- consults this table on every task claim and refuses the claim when the
-- pass is expired, revoked, or exhausted.
--
-- Downscoping (sub-issues can only narrow parent scope, never widen) is
-- modelled via parent_pass_id; the actual narrowing check lives in the
-- agentpass package, not in a CHECK constraint, because scope is JSONB.
--
-- spend_ceiling is stored in micro-USD (1 USD = 1_000_000) so we can
-- compare against task_usage_daily_rollup.cost_micros without conversion.
-- NULL means "no ceiling" (use the workspace default if any).
--
-- Idempotent: uses IF NOT EXISTS so re-running up after a down is a no-op.

CREATE TABLE IF NOT EXISTS cerebro_agent_pass (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- Issuer (who granted the pass)
    issuer_type          TEXT NOT NULL,
    issuer_id            UUID NOT NULL,
    CONSTRAINT cerebro_agent_pass_issuer_type_check CHECK (
        issuer_type IN ('member', 'agent', 'system')
    ),

    -- Holder (the agent the pass is granted to)
    agent_id             UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,

    -- Anchor issue. The pass covers this issue and its descendants
    -- (sub-issues), unless a child issue has its own narrower pass.
    issue_id             UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,

    -- Downscoping chain. NULL when the pass is the root mandate.
    parent_pass_id       UUID REFERENCES cerebro_agent_pass(id) ON DELETE CASCADE,

    -- Scope: free-form JSONB so we can grow the schema (allowed_tools,
    -- allowed_capabilities, allowed_models, etc.) without a migration.
    -- Empty object = "no scope restrictions imposed by this pass" (the
    -- parent or workspace policy still applies).
    scope                JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Spend ceiling in micro-USD. NULL = no ceiling on this pass.
    spend_ceiling_micros BIGINT,
    CONSTRAINT cerebro_agent_pass_spend_ceiling_check CHECK (
        spend_ceiling_micros IS NULL OR spend_ceiling_micros >= 0
    ),

    -- Lifecycle.
    status               TEXT NOT NULL DEFAULT 'active',
    CONSTRAINT cerebro_agent_pass_status_check CHECK (
        status IN ('active', 'revoked', 'expired', 'exhausted')
    ),

    -- Time window.
    issued_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at           TIMESTAMPTZ,

    -- Revocation audit.
    revoked_by_type      TEXT,
    revoked_by_id        UUID,
    revoked_at           TIMESTAMPTZ,
    revoke_reason        TEXT,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Lookup path used by the pre-run gate: "is there an active pass for this
-- (agent, issue) right now?". Partial index keeps the index small because
-- most rows will eventually be in a terminal status.
CREATE INDEX IF NOT EXISTS idx_cerebro_agent_pass_active_agent_issue
    ON cerebro_agent_pass (agent_id, issue_id)
    WHERE status = 'active';

-- Workspace scan used by the admin UI ("show all active passes in this
-- workspace"). Same partial-index trick.
CREATE INDEX IF NOT EXISTS idx_cerebro_agent_pass_active_workspace
    ON cerebro_agent_pass (workspace_id, issued_at DESC)
    WHERE status = 'active';

-- Downscoping chain traversal (parent → children).
CREATE INDEX IF NOT EXISTS idx_cerebro_agent_pass_parent
    ON cerebro_agent_pass (parent_pass_id)
    WHERE parent_pass_id IS NOT NULL;
