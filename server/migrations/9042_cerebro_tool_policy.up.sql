-- 9042_cerebro_tool_policy: per-tool, per-layer Allow/Ask/Deny/Inherit settings.
--
-- FIR-2230 phase 1 (persistence). The pure resolver in
-- server/internal/cerebro/toolpolicy/chain.go folds a four-layer chain
-- (Runtime -> Agent -> Group -> User) into one Effective verdict per tool. This
-- table is where each layer's explicit choice lives, so the chain can be
-- assembled from the database instead of computed from nothing.
--
-- One row per (workspace, tool, layer, subject):
--   layer='runtime', subject_id=agent_runtime.id  -- the machine default
--   layer='agent',   subject_id=agent.id          -- the per-agent override
--   layer='group',   subject_id=cerebro_group.id  -- the shared team policy
--   layer='user',    subject_id="user".id         -- the requester ceiling
--
-- subject_id is polymorphic across four parent tables, so it carries no foreign
-- key (the same column points at a runtime, an agent, a group, or a user
-- depending on layer). The 'layer' CHECK keeps the discriminator honest.
--
-- 'setting' is one of inherit/allow/ask/deny (toolpolicy.Setting). An absent row
-- is treated as 'inherit' by the resolver, so storing 'inherit' explicitly is
-- only useful to let the UI render a deliberate "follow the layer below" choice.
--
-- 'tool_key' is the stable identifier the resolver keys on: a CLI tool name
-- (e.g. 'add_comment') or a namespaced MCP action (e.g. 'bigquery.query'). Every
-- individual tool and MCP action is governed on its own line, never as a server-
-- level clump.

CREATE TABLE IF NOT EXISTS cerebro_tool_policy (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tool_key TEXT NOT NULL,
    layer TEXT NOT NULL,
    subject_id UUID NOT NULL,
    setting TEXT NOT NULL,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_tool_policy_layer_known
        CHECK (layer IN ('runtime', 'agent', 'group', 'user')),
    CONSTRAINT cerebro_tool_policy_setting_known
        CHECK (setting IN ('inherit', 'allow', 'ask', 'deny')),
    CONSTRAINT cerebro_tool_policy_unique
        UNIQUE (workspace_id, tool_key, layer, subject_id)
);

-- The hot path resolves one tool for one (runtime, agent, user, groups) context,
-- so the lookup keys on (workspace, tool).
CREATE INDEX IF NOT EXISTS idx_cerebro_tool_policy_lookup
    ON cerebro_tool_policy (workspace_id, tool_key);

-- The admin table renders every tool's setting for one subject (e.g. the "This
-- agent" column), so a per-subject scan is keyed too.
CREATE INDEX IF NOT EXISTS idx_cerebro_tool_policy_subject
    ON cerebro_tool_policy (workspace_id, layer, subject_id);
