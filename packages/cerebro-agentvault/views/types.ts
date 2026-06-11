// Domain types mirroring server/internal/cerebro/agentvault/store.go (TECH-3196).

export type AgentVaultRole = "read-only" | "member" | "admin";

export interface AgentVaultAccess {
  agent_id: string;
  vault: string;
  role: AgentVaultRole;
  updated_at: string;
}
