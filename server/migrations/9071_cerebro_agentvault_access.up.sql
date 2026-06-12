-- 9071_cerebro_agentvault_access: per-agent key-access table for Agent Vault (TECH-3196).
--
-- Implements the TECH-2962 access model: the workspace admin controls ONE table
-- mapping each agent to the Agent Vault boxes (vaults) it may use, and at which
-- role. Agents cannot grant themselves access. Brokering happens server-side over
-- the Agent Vault internal path; the agent never holds the underlying secret.
--
-- role is the per-vault Agent Vault role: read-only (default), member, admin.

CREATE TABLE IF NOT EXISTS cerebro_agentvault_agent_access (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id     UUID        NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    vault        TEXT        NOT NULL,
    role         TEXT        NOT NULL DEFAULT 'read-only',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_agentvault_access_role_known
        CHECK (role IN ('read-only', 'member', 'admin')),
    CONSTRAINT cerebro_agentvault_access_vault_not_blank
        CHECK (length(trim(vault)) > 0),
    CONSTRAINT cerebro_agentvault_access_unique
        UNIQUE (workspace_id, agent_id, vault)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_agentvault_access_agent
    ON cerebro_agentvault_agent_access (workspace_id, agent_id);
