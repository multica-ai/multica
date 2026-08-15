-- Allow a public_to agent to name another same-workspace agent as an exact
-- invocation principal. Same-workspace / active-agent validation stays in the
-- application layer because this polymorphic table deliberately has no FKs.
ALTER TABLE agent_invocation_target
    DROP CONSTRAINT IF EXISTS agent_invocation_target_target_type_check;

ALTER TABLE agent_invocation_target
    ADD CONSTRAINT agent_invocation_target_target_type_check
    CHECK (target_type IN ('workspace', 'member', 'team', 'agent'));

COMMENT ON TABLE agent_invocation_target IS
    'Allow-list of who may invoke a public_to agent. Agent targets grant only the exact active same-workspace source agent and are non-transitive; workspace/member/team retain their existing semantics. Rows only matter when agent.permission_mode = public_to. No DB foreign keys: relationships are validated and cleaned up in the application layer.';
