-- 9096_cerebro_credential_policy: per-credential, per-layer Allow/Deny/Inherit
-- grants — credentials as a first-class permission type (FIR-1479).
--
-- Jesper's model: a "credential" is one Agent Vault box (e.g. 'bigquery',
-- 'personal-jesper', a per-repo box). It is assigned to an actor (agent /
-- person / group) the same way a tool is assigned today via a tool grant: a
-- ticked box = access to exactly that one vault box, nothing more
-- (least-privilege — granting opens nothing else up).
--
-- This table is the persistence layer that the pure resolver in
-- server/internal/cerebro/credentialpolicy/chain.go folds into one Effective
-- verdict per (credential, actor) context. It deliberately mirrors
-- cerebro_tool_policy (9042 + 9052 + 9088) so the permissions interface can
-- render and resolve credentials through the same five-layer chain it already
-- uses for tools:
--
--   layer='workspace', subject_id=workspace.id      -- the workspace-wide root
--   layer='runtime',   subject_id=agent_runtime.id  -- the machine default
--   layer='agent',     subject_id=agent.id          -- the per-agent override
--   layer='group',     subject_id=cerebro_group.id  -- the shared team policy
--   layer='user',      subject_id="user".id         -- the requester ceiling
--   layer='system',    subject_id="user".id         -- the no-human mandate
--
-- subject_id is polymorphic across those parent tables, so it carries no
-- foreign key (the same column points at a workspace, runtime, agent, group, or
-- user depending on layer). The 'layer' CHECK keeps the discriminator honest.
--
-- 'setting' is one of inherit/allow/deny (credentialpolicy.Setting). Unlike
-- tool policy there is no 'ask' — a credential is either reachable or not; the
-- approval prompt lives on the *action* taken with the underlying secret
-- (reveal/rotate), governed separately by cerebro_credential's own policy.
-- An absent row is treated as 'inherit' by the resolver, which folds to the
-- deny-by-default base: a box no layer has explicitly granted is unreachable.
--
-- 'credential_key' is the stable identifier the resolver keys on: the Agent
-- Vault box name (e.g. 'bigquery'). Keying on the box name (not a
-- cerebro_credential row id) keeps the grant aligned with the "one box = one
-- credential" mental model and survives credentials being re-registered.

CREATE TABLE IF NOT EXISTS cerebro_credential_policy (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    credential_key TEXT NOT NULL,
    layer TEXT NOT NULL,
    subject_id UUID NOT NULL,
    setting TEXT NOT NULL,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_credential_policy_layer_known
        CHECK (layer IN ('workspace', 'runtime', 'agent', 'group', 'user', 'system')),
    CONSTRAINT cerebro_credential_policy_setting_known
        CHECK (setting IN ('inherit', 'allow', 'deny')),
    CONSTRAINT cerebro_credential_policy_unique
        UNIQUE (workspace_id, credential_key, layer, subject_id)
);

-- The hot path resolves one credential for one (workspace, runtime, agent,
-- user, groups) context, so the lookup keys on (workspace, credential).
CREATE INDEX IF NOT EXISTS idx_cerebro_credential_policy_lookup
    ON cerebro_credential_policy (workspace_id, credential_key);

-- The permissions table renders every credential's setting for one subject
-- (e.g. the "This agent" column), so a per-subject scan is keyed too.
CREATE INDEX IF NOT EXISTS idx_cerebro_credential_policy_subject
    ON cerebro_credential_policy (workspace_id, layer, subject_id);
