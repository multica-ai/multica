-- 9146_cerebro_access_decision_ledger: append-only observations comparing the
-- live Gateway decision with the canonical shadow decision.
--
-- Additive and observational. No runtime gate reads this table, and a missing
-- row can never grant or deny a tool call.

CREATE TABLE IF NOT EXISTS cerebro_access_decision_ledger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    on_behalf_of_user_id UUID,
    task_id UUID,
    issue_id UUID,
    observed_tool_name TEXT NOT NULL,
    canonical_capability_id TEXT,
    legacy_decision TEXT NOT NULL,
    legacy_path TEXT NOT NULL,
    shadow_decision TEXT NOT NULL,
    policy_decision TEXT NOT NULL,
    evidence_level TEXT NOT NULL,
    differs BOOLEAN NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_access_decision_observed_tool_not_blank
        CHECK (length(trim(observed_tool_name)) > 0),
    CONSTRAINT cerebro_access_decision_legacy_path_not_blank
        CHECK (length(trim(legacy_path)) > 0),
    CONSTRAINT cerebro_access_decision_legacy_known
        CHECK (legacy_decision IN ('allow', 'deny')),
    CONSTRAINT cerebro_access_decision_shadow_known
        CHECK (shadow_decision IN ('allow', 'deny')),
    CONSTRAINT cerebro_access_decision_policy_known
        CHECK (policy_decision IN ('allow', 'ask', 'deny', 'error')),
    CONSTRAINT cerebro_access_decision_evidence_known
        CHECK (evidence_level IN ('declared', 'discovered', 'verified')),
    CONSTRAINT cerebro_access_decision_reason_not_blank
        CHECK (length(trim(reason)) > 0)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_access_decision_report
    ON cerebro_access_decision_ledger (
        workspace_id, agent_id, runtime_id, canonical_capability_id, created_at DESC
    );

CREATE INDEX IF NOT EXISTS idx_cerebro_access_decision_diffs
    ON cerebro_access_decision_ledger (workspace_id, created_at DESC)
    WHERE differs = true;
