-- FIR-3091 punkt 8 fase 3 — usage log for tool-policy enforcement.
--
-- Every time a permission is actually enforced at one of the live gates
-- (mention trigger gate, repo checkout, agent-browser sandbox, connection
-- per-tool, gateway tool-policy, local-CLI enforce, the member HTTP-action
-- gates) one row lands here, so the per-permission detail page can show "every
-- time this permission was used" for a tool. Append-only, best-effort after
-- the decision is applied — a missing row is a gap in the log, never a blocked
-- action. Recording starts the day this ships; there is no backfill source.
--
-- Shape mirrors cerebro_tool_policy_audit (fase 2) so the two logs read the
-- same: subject + created_at, with the usage-specific dimensions added
-- (enforcement_point, the concrete resource, the applied decision, and the
-- layer that decided it).

CREATE TABLE IF NOT EXISTS cerebro_tool_policy_usage (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tool_key          TEXT NOT NULL,
    -- Which gate applied the decision (e.g. 'mention_gate', 'repo_checkout',
    -- 'agent_browser_sandbox', 'gateway_tool', 'local_cli'). Free-form so a new
    -- gate can record without a migration.
    enforcement_point TEXT NOT NULL,
    -- Who the decision was applied to: the agent making the call or the member
    -- performing the HTTP action. 'system' covers a human-less run.
    subject_type      TEXT NOT NULL CHECK (subject_type IN ('member', 'agent', 'system')),
    subject_id        UUID,
    -- The concrete resource of the call (repo URL, tool name, target agent id);
    -- '' when the gate is capability-wide.
    resource          TEXT NOT NULL DEFAULT '',
    decision          TEXT NOT NULL CHECK (decision IN ('allow', 'ask', 'deny')),
    -- The layer whose rule decided ('workspace', 'group', …); '' means no
    -- explicit rule matched and the gate's base/baseline answered.
    decided_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The detail page reads one tool's usage newest-first.
CREATE INDEX IF NOT EXISTS idx_cerebro_tool_policy_usage_tool
    ON cerebro_tool_policy_usage (workspace_id, tool_key, created_at DESC);
