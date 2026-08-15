-- Old binaries do not understand agent principals. Remove those additive rows
-- before restoring the previous CHECK so rollback remains fail-closed rather
-- than widening an agent to workspace access.
DELETE FROM agent_invocation_target WHERE target_type = 'agent';

ALTER TABLE agent_invocation_target
    DROP CONSTRAINT IF EXISTS agent_invocation_target_target_type_check;

ALTER TABLE agent_invocation_target
    ADD CONSTRAINT agent_invocation_target_target_type_check
    CHECK (target_type IN ('workspace', 'member', 'team'));

COMMENT ON TABLE agent_invocation_target IS
    'Allow-list of who may invoke a public_to agent (MUL-3963). One row per (agent, target_type, target); targets stack and canInvokeAgent OR-matches. workspace rows store the agent workspace_id in target_id; member rows store the user id; team rows are reserved and inert in V1. Rows only matter when agent.permission_mode = public_to. No DB foreign keys: agent_id / created_by / member target_id relationships are maintained in the application layer.';
