// Wire shape returned by the cerebro agent-pass admin API
// (server/internal/cerebro/agentpass/handler.go). Mirrors the backend
// passResponse exactly — keep the two in sync.

export type AgentPassStatus = "active" | "revoked" | "expired" | "exhausted";

export interface AgentPass {
  id: string;
  workspace_id: string;
  issuer_type: string;
  issuer_id: string;
  agent_id: string;
  issue_id: string;
  parent_pass_id?: string;
  scope: Record<string, unknown>;
  spend_ceiling_micros?: number | null;
  status: AgentPassStatus;
  issued_at: string;
  expires_at?: string;
  revoked_by_type?: string;
  revoked_by_id?: string;
  revoked_at?: string;
  revoke_reason?: string;
  created_at: string;
  updated_at: string;
}

export interface AgentPassListResponse {
  passes: AgentPass[];
}

// Wire shape for POST /api/cerebro/agent-passes. Scope is parsed JSON.
export interface CreateAgentPassRequest {
  agent_id: string;
  issue_id: string;
  parent_pass_id?: string;
  scope?: Record<string, unknown>;
  spend_ceiling_micros?: number | null;
  expires_at?: string;
}

export interface RevokeAgentPassRequest {
  reason?: string;
}

// Stable list of statuses for the UI to render filters / badges. The
// backend uses identical strings; if a new status is added there we want
// the UI to ignore it rather than crash, so consumers should optional-
// chain the lookup.
export const AGENT_PASS_STATUSES: AgentPassStatus[] = [
  "active",
  "revoked",
  "expired",
  "exhausted",
];

// Convert micro-USD (the unit stored in the DB) to a human dollar string.
// Returns "—" when the ceiling is not set so the UI can show a sentinel
// without branching at every render site.
export function formatSpendCeiling(micros?: number | null): string {
  if (micros == null) return "—";
  const dollars = micros / 1_000_000;
  return dollars.toLocaleString(undefined, {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 2,
  });
}
