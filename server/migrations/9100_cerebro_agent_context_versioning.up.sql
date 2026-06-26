-- Cerebro feature (FIR-1775, "Agent Office"): versioning + governance for an
-- agent's full runtime context, mirroring the skill model in 9013_skill_ownership.
--
-- Unlike a skill (a single content blob + files), an agent's context is a
-- COMPOSITE: instructions, bound skills, model, thinking_level, mcp_config,
-- custom_args, runtime_config, persona_sandbox, and the NAMES (never values) of
-- custom_env keys. A version snapshot stores that whole composite as one JSONB
-- document so we can diff between versions and roll back.
--
-- Secret values are intentionally NOT versioned here — only key names. Values
-- stay in the agent env endpoint / Infisical / agentvault.

-- Governance columns on the agent row: who owns/approves context changes, and
-- the pointer to the most recent snapshot. Separate from owner_id (the agent's
-- human owner) so context governance can be delegated independently.
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS context_owner_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS context_approver_ids UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS context_version TEXT NOT NULL DEFAULT '1.0.0';

CREATE INDEX IF NOT EXISTS idx_agent_context_owner ON agent(context_owner_id);

-- Append-only snapshots of the full composite context at the time of merge.
-- agent.context_version points at the most recent entry here.
CREATE TABLE IF NOT EXISTS agent_context_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    -- snapshot keys: instructions, description, model, thinking_level,
    -- mcp_config, custom_args, runtime_config, persona_sandbox, skill_ids[],
    -- custom_env_keys[] (NAMES only — never secret values).
    snapshot JSONB NOT NULL DEFAULT '{}',
    description TEXT NOT NULL DEFAULT '',
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (agent_id, version)
);

CREATE INDEX IF NOT EXISTS idx_agent_context_version_agent
    ON agent_context_version(agent_id);

-- A proposed edit to an agent's context, not yet merged. Stores the full
-- proposed composite so it can be reviewed independently of the live agent.
-- status transitions: pending -> approved|rejected, and approved -> merged
-- when the owner/approver applies it. proposed_by is polymorphic (user or
-- agent id, no FK) to match skill_change_request after the agent-proposer
-- migration (9098_cerebro_skill_cr_agent_proposer).
CREATE TABLE IF NOT EXISTS agent_change_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    base_version TEXT NOT NULL,
    proposed_version TEXT NOT NULL,
    proposed_snapshot JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'merged')),
    proposed_by UUID NOT NULL,
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    review_comment TEXT NOT NULL DEFAULT '',
    work_session_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_change_request_agent
    ON agent_change_request(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_change_request_status
    ON agent_change_request(status)
    WHERE status = 'pending';

-- Backfill: context owner = the agent's existing human owner so the governance
-- rule is enforceable from day one; ownerless agents fall back to the
-- workspace owner/admin in the permission hook (same as skills).
UPDATE agent SET context_owner_id = owner_id
WHERE context_owner_id IS NULL AND owner_id IS NOT NULL;

-- Backfill: every existing agent gets an initial v1.0.0 snapshot of its current
-- composite so the version-history view renders and future change requests have
-- a base_version to diff against. custom_env is reduced to its key names only.
INSERT INTO agent_context_version (agent_id, version, snapshot, description, created_by, created_at)
SELECT
    a.id,
    a.context_version,
    jsonb_build_object(
        'instructions',    a.instructions,
        'description',     a.description,
        'model',           a.model,
        'thinking_level',  a.thinking_level,
        'mcp_config',      a.mcp_config,
        'custom_args',     a.custom_args,
        'runtime_config',  a.runtime_config,
        'persona_sandbox', a.persona_sandbox,
        'skill_ids', COALESCE(
            (SELECT jsonb_agg(s.skill_id ORDER BY s.skill_id)
             FROM agent_skill s WHERE s.agent_id = a.id),
            '[]'::jsonb),
        'custom_env_keys', COALESCE(
            (SELECT jsonb_agg(k ORDER BY k)
             FROM jsonb_object_keys(COALESCE(a.custom_env, '{}'::jsonb)) AS k),
            '[]'::jsonb)
    ),
    'Initial snapshot (backfill)',
    a.owner_id,
    a.created_at
FROM agent a
WHERE NOT EXISTS (
    SELECT 1 FROM agent_context_version v WHERE v.agent_id = a.id
);
