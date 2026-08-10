-- Agent A2A invocation mode (NEX-24): an independent, owner-authored axis that
-- controls whether AGENT principals may trigger this agent, orthogonal to
-- permission_mode (which keeps governing MEMBER callers). It deliberately does
-- NOT govern SYSTEM callers: system keeps its pre-existing judgment unchanged
-- (`public_to workspace` via the workspaceBroad exception, everything else
-- fail-closed).
--
-- The column default is 'default' = status-quo fail-closed: A2A calls keep
-- being judged by the top-of-chain human originator against permission_mode,
-- so the historical "no human originator -> deny" behavior is preserved with
-- zero migration of existing rows (they all land on 'default'). Keeping a real
-- 'default' enum value is a forward-compatibility choice: old clients / unknown
-- values degrade to 'default' (behavior zero-change) instead of having no safe
-- landing spot. There is NO empty-string meaning — '' is not a legal value.
--
-- Values:
--   * 'default'         -> unset (status quo; fail-closed)
--   * 'any_agent'       -> any agent principal may invoke (not system)
--   * 'squad_leaders'   -> only agent principals leading a squad may invoke
--   * 'specific_agents' -> only the agents named in agent_invocation_grant
--                          may invoke
ALTER TABLE agent
    ADD COLUMN a2a_invocation_mode TEXT NOT NULL DEFAULT 'default'
        CHECK (a2a_invocation_mode IN ('default', 'any_agent', 'squad_leaders', 'specific_agents'));

COMMENT ON COLUMN agent.a2a_invocation_mode IS
    'A2A invocation mode (NEX-24). Independent of permission_mode; only governs AGENT callers (system is unaffected). default = unset = status-quo fail-closed (A2A judged by the top-of-chain human originator); any_agent = any agent principal may invoke; squad_leaders = only agent principals leading a squad; specific_agents = only agents on agent_invocation_grant. Unknown values are treated as default (forward-compatible).';
