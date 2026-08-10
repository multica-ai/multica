-- A2A invocation whitelist (NEX-24): rows back agent.a2a_invocation_mode =
-- 'specific_agents'. One row per (agent_id, grantee_agent_id); the composite
-- PRIMARY KEY (agent_id, grantee_agent_id) is the only index needed for the
-- forward lookup (by agent_id).
--
-- NO foreign keys by design (Multica migration rule): relationships are
-- maintained in the application layer. Cleanup for agent hard-deletes and for
-- a removed grantee agent uses the dedicated application-layer queries
-- DeleteAgentInvocationGrantsByAgent / DeleteAgentInvocationGrantsByGrantee
-- (wired in Stage 3 cleanup, mirroring agent_invocation_target).
CREATE TABLE agent_invocation_grant (
    agent_id          UUID NOT NULL,
    grantee_agent_id  UUID NOT NULL,
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, grantee_agent_id)
);

COMMENT ON TABLE agent_invocation_grant IS
    'A2A invocation whitelist (NEX-24): which agents may trigger the owning agent when agent.a2a_invocation_mode = specific_agents. agent_id is the target (called) agent; grantee_agent_id is the caller allowed to invoke it. No DB foreign keys; relationships are maintained in the application layer (see migration comment).';
