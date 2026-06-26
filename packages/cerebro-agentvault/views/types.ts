// Domain types mirroring server/internal/cerebro/agentvault/store.go (TECH-3196).

export type AgentVaultRole = "read-only" | "member" | "admin";

export interface AgentVaultAccess {
  agent_id: string;
  vault: string;
  role: AgentVaultRole;
  updated_at: string;
}

// One Agent Vault box as listed by GET /api/workspaces/{id}/agentvault/vaults
// (FIR-1739 v1). Used to populate the vault-picker on the credential Permissions
// row; only id + name are surfaced.
export interface AgentVaultBox {
  id: string;
  name: string;
}
