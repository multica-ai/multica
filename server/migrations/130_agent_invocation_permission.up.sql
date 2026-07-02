-- Agent invocation permission system, V1 (MUL-3963, parent MUL-3715 Composio epic).
--
-- Splits "who may TRIGGER/INVOKE an agent" out of the overloaded `visibility`
-- column into an explicit, extensible model:
--
--   * agent.permission_mode: 'private' | 'public_to'
--       - private   -> only the agent owner may invoke. Workspace admin does
--         NOT bypass this any more (that was the privacy hole described in the
--         issue: an admin could invoke someone's private agent and read their
--         mailbox via that agent's Composio connections).
--       - public_to -> an owner-configured allow-list of invocation targets
--         (see agent_invocation_target) decides who may invoke.
--
--   * agent_invocation_target: the allow-list rows for public_to agents.
--       - target_type = 'workspace' -> every workspace member may invoke.
--       - target_type = 'member'    -> only the specific user may invoke.
--       - target_type = 'team'      -> reserved for the future team concept;
--         stored but NOT effective in V1 (no team membership source yet).
--
-- `visibility` is intentionally left in place and kept in sync as a DERIVED
-- legacy field by the API layer (private/public_to-member-only -> 'private',
-- public_to-workspace -> 'workspace'), so old clients never see a permission
-- WIDENING. All new trigger/dispatch decisions read permission_mode + targets
-- via canInvokeAgent; visibility is no longer an authorization source.

-- ----------------------------------------------------------------------------
-- agent.permission_mode
-- ----------------------------------------------------------------------------
ALTER TABLE agent
    ADD COLUMN permission_mode TEXT NOT NULL DEFAULT 'private'
        CHECK (permission_mode IN ('private', 'public_to'));

COMMENT ON COLUMN agent.permission_mode IS
    'Agent invocation permission mode (MUL-3963). private = owner only; public_to = allow-list in agent_invocation_target. Replaces visibility as the authorization source for triggering runs; visibility is now a derived legacy field. Default private = deny-by-default.';

-- ----------------------------------------------------------------------------
-- agent_invocation_target
-- ----------------------------------------------------------------------------
--
-- target_id is polymorphic (workspace_id / user_id / future team_id) so it
-- carries NO foreign key by design. For 'workspace' rows we store the agent's
-- workspace_id (rather than NULL) so the UNIQUE(agent_id, target_type,
-- target_id) constraint dedups cleanly — Postgres treats NULLs as distinct,
-- which would otherwise allow duplicate workspace rows. member/team rows must
-- carry a target_id.
CREATE TABLE agent_invocation_target (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK (target_type IN ('workspace', 'member', 'team')),
    target_id   UUID NOT NULL,
    created_by  UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, target_type, target_id)
);

COMMENT ON TABLE agent_invocation_target IS
    'Allow-list of who may invoke a public_to agent (MUL-3963). One row per (agent, target_type, target). workspace rows store the agent workspace_id in target_id; member rows store the user id; team rows are reserved and inert in V1. Rows only matter when agent.permission_mode = public_to.';

CREATE INDEX agent_invocation_target_agent_id_idx
    ON agent_invocation_target(agent_id);

-- Reverse lookup: "which agents can this member invoke" style filters and the
-- member-target cleanup on user removal.
CREATE INDEX agent_invocation_target_target_idx
    ON agent_invocation_target(target_type, target_id);

-- ----------------------------------------------------------------------------
-- Backfill from legacy visibility (lossless migration)
-- ----------------------------------------------------------------------------
--   visibility = 'private'   -> permission_mode = 'private' (column default), no target
--   visibility = 'workspace' -> permission_mode = 'public_to' + one workspace target
UPDATE agent SET permission_mode = 'public_to' WHERE visibility = 'workspace';

INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
SELECT id, 'workspace', workspace_id, NULL
FROM agent
WHERE visibility = 'workspace'
ON CONFLICT (agent_id, target_type, target_id) DO NOTHING;
