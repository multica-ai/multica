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
--         (see agent_invocation_target) decides who may invoke. Targets stack:
--         one agent may combine a workspace target + specific members + (future)
--         teams; canInvokeAgent admits an actor matching ANY target (OR).
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
-- NO foreign keys by design (Multica migration rule, matching the MUL-3515 §4
-- channel_* generalization): relationships are maintained in the application
-- layer, not by the database. Concretely:
--   * agent_id  — cleaned up alongside agent hard-deletes
--     (DeleteAgentInvocationTargetsByArchivedRuntimeAgents runs before
--     DeleteArchivedAgentsByRuntime in the runtime-delete tx). Agents are
--     normally soft-archived, so rows persist across archive/restore.
--   * created_by / member target_id — pruned on member removal via
--     DeleteAgentInvocationTargetsByMember inside the revoke-member tx, so a
--     re-invited user does not inherit stale invocation grants.
--
-- target_id is polymorphic (workspace_id / user_id / future team_id) so it
-- carries NO FK anyway. For 'workspace' rows we store the agent's workspace_id
-- (rather than NULL) so the UNIQUE(agent_id, target_type, target_id) constraint
-- dedups cleanly — Postgres treats NULLs as distinct, which would otherwise
-- allow duplicate workspace rows. member/team rows must carry a target_id.
CREATE TABLE agent_invocation_target (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL,
    target_type TEXT NOT NULL CHECK (target_type IN ('workspace', 'member', 'team')),
    target_id   UUID NOT NULL,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, target_type, target_id)
);

COMMENT ON TABLE agent_invocation_target IS
    'Allow-list of who may invoke a public_to agent (MUL-3963). One row per (agent, target_type, target); targets stack and canInvokeAgent OR-matches. workspace rows store the agent workspace_id in target_id; member rows store the user id; team rows are reserved and inert in V1. Rows only matter when agent.permission_mode = public_to. No DB foreign keys: agent_id / created_by / member target_id relationships are maintained in the application layer (see migration comment).';

CREATE INDEX agent_invocation_target_agent_id_idx
    ON agent_invocation_target(agent_id);

-- Reverse lookup: member-target cleanup on user removal, and "which agents can
-- this member invoke" style filters.
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

-- ----------------------------------------------------------------------------
-- Refresh two Composio-era column comments to the MUL-3963 rules (the overlay
-- now FOLLOWS invocation permission and uses the agent OWNER's connection; the
-- old "originator must equal owner" gate is gone). These COMMENT ON COLUMN
-- statements also drive the sqlc-generated doc comments in models.go.
-- ----------------------------------------------------------------------------
COMMENT ON COLUMN agent.composio_toolkit_allowlist IS
    'Composio toolkit slugs this agent is allowed to mount as MCP. NULL or empty array = no MCP overlay. Mounted for any run that passes the agent invocation-permission gate (MUL-3963); the overlay uses the agent OWNER''s active Composio connection, so sharing the agent (public_to) shares these apps with whoever may invoke it. No longer gated on originator == owner. Stored as TEXT[] so the dispatch path can intersect against the owner''s active connections with a single SQL ANY() filter.';

COMMENT ON COLUMN agent_task_queue.originator_user_id IS
    'Top-of-chain human originator for this run. For human-triggered tasks (comment by a member, chat, quick-create) equals that member. For agent-fanout tasks inherited from the parent task''s originator_user_id via comment.source_task_id. NULL when no human is in the chain (autopilot, system-driven). Used by canInvokeAgent to judge A2A by the originator; the Composio overlay now follows invocation permission and uses the agent owner''s connection, so this is audit/attribution + A2A gating, NOT a Composio owner==originator gate (MUL-3963).';
