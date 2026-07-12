// Agent Office (FIR-1775 Phase 4) — defensive normalization of the context
// observability overview. The API method returns the raw body; the desktop app
// is older than the server it talks to (see CLAUDE.md "API Response
// Compatibility"), so every field is coerced to a safe default here before it
// reaches the render layer. A drifted or partial response degrades to zeros,
// never a white screen.

import type {
  AgentContextObservability,
  AgentContextChangeRequestCounts,
  AgentContextApproverStat,
  AgentContextDriftSummary,
} from "@multica/core/types";

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function normalizeCounts(raw: unknown): AgentContextChangeRequestCounts {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    pending: num(r.pending),
    approved: num(r.approved),
    rejected: num(r.rejected),
    merged: num(r.merged),
    total: num(r.total),
  };
}

function normalizeDrift(raw: unknown): AgentContextDriftSummary {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    total: num(r.total),
    errors: num(r.errors),
    warnings: num(r.warnings),
    infos: num(r.infos),
  };
}

function normalizeApprover(raw: unknown): AgentContextApproverStat {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    user_id: str(r.user_id),
    name: str(r.name),
    approved: num(r.approved),
    merged: num(r.merged),
    rejected: num(r.rejected),
    total: num(r.total),
  };
}

/**
 * normalizeAgentObservability coerces any (possibly drifted) API body into a
 * fully-populated AgentContextObservability. Non-array approvers become [];
 * missing counts become zero.
 */
export function normalizeAgentObservability(
  raw: unknown,
): AgentContextObservability {
  const r = (raw ?? {}) as Record<string, unknown>;
  const approvers = Array.isArray(r.approvers)
    ? r.approvers.map(normalizeApprover)
    : [];
  const lastChanged = r.last_changed_at;
  return {
    agent_id: str(r.agent_id),
    agent_name: str(r.agent_name),
    context_version: str(r.context_version),
    version_count: num(r.version_count),
    versions_last_30d: num(r.versions_last_30d),
    last_changed_at: typeof lastChanged === "string" ? lastChanged : null,
    change_requests: normalizeCounts(r.change_requests),
    approvers,
    drift: normalizeDrift(r.drift),
  };
}
