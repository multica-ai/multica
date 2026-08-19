import type { HealthScore, WorkspaceStatus } from "./types";

// Pure derivation functions per multica-admin-dashboard-plan.md §4
// ("Derived fields"). Kept local to apps/admin: nothing else in the
// monorepo consumes this logic yet, so extracting it to packages/core
// would be speculative generality (see CLAUDE.md persona rule).

/**
 * A workspace's status is derived from its agents' live state, not stored
 * directly:
 *   - "error"  any agent in the workspace is currently in the `error` state
 *   - "active" any runtime is online, or one has reported activity within
 *              the last hour
 *   - "idle"   otherwise
 *
 * This mirrors the SQL CASE expression in lib/queries.ts (kept in SQL there
 * so it can also drive filtering/sorting) — this copy exists for unit
 * testing the rule in isolation and for call sites that already have the
 * raw fields in hand (e.g. after merging LiteLLM data).
 */
export function deriveStatus(input: {
  hasErrorAgent: boolean;
  anyRuntimeOnline: boolean;
  lastSeenAt: Date | null;
}): WorkspaceStatus {
  if (input.hasErrorAgent) return "error";
  const recentlySeen =
    input.lastSeenAt !== null &&
    Date.now() - input.lastSeenAt.getTime() < 60 * 60 * 1000;
  if (input.anyRuntimeOnline || recentlySeen) return "active";
  return "idle";
}

/** completed / (completed + failed), as a 0-100 percentage. Null when there's
 * no resolved-task history yet — the UI must render "Not enough data", never
 * a fabricated percentage (DESIGN.md anti-pattern: no invented metrics). */
export function deriveSuccessRate(completed: number, failed: number): number | null {
  const total = completed + failed;
  if (total === 0) return null;
  return Math.round((completed / total) * 1000) / 10;
}

/**
 * Composite health score from success rate + current status + average issue
 * resolution time. Thresholds are intentionally simple and documented rather
 * than a hidden formula:
 *   - "critical": workspace status is "error", or success rate < 70%
 *   - "warning":  success rate 70–90%, or resolution time > 24h
 *   - "good":     everything else (including "not enough data" — a workspace
 *                 with no history isn't unhealthy, just unproven)
 */
export function deriveHealth(input: {
  status: WorkspaceStatus;
  successRate: number | null;
  avgResolutionHours: number | null;
}): HealthScore {
  if (input.status === "error") return "critical";
  if (input.successRate !== null && input.successRate < 70) return "critical";
  if (input.successRate !== null && input.successRate < 90) return "warning";
  if (input.avgResolutionHours !== null && input.avgResolutionHours > 24) return "warning";
  return "good";
}
