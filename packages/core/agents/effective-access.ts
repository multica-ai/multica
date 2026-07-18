import type { AgentInvocationTarget, AgentPermissionMode } from "../types";

/**
 * The three effective access-scope states shown in the agents list, derived
 * from the authoritative `permission_mode` + `invocation_targets` (MUL-3963).
 * The legacy `visibility` field is a lossy two-state projection of these — a
 * `public_to` agent scoped to specific people maps to `visibility: "private"`,
 * indistinguishable from a truly owner-only agent — so the list shows this
 * three-state value instead.
 *
 * Mapping mirrors the server's `canInvokeAgent` gate
 * (`server/internal/handler/agent_access.go`):
 *   - owner-only      = `private` (only the owner may invoke)
 *   - workspace       = `public_to` with a workspace target (any member/agent/system)
 *   - specific-people = `public_to` without a workspace target (member/team targets)
 */
export type AccessScope = "workspace" | "specific-people" | "owner-only";

/**
 * Derive the effective access scope from an agent's permission fields. Fails
 * safe to "owner-only" when `permission_mode` is absent (the runtime may omit
 * these on legacy self-host backends or stale caches); a `public_to` agent
 * with absent `invocation_targets` stays "specific-people".
 */
export function effectiveAccessScope(
  permissionMode: AgentPermissionMode | undefined | null,
  invocationTargets: readonly AgentInvocationTarget[] | undefined | null,
): AccessScope {
  if (permissionMode !== "public_to") {
    return "owner-only";
  }
  if ((invocationTargets ?? []).some((t) => t.target_type === "workspace")) {
    return "workspace";
  }
  return "specific-people";
}

/** All possible effective access-scope values, in display order. */
export const ALL_ACCESS_SCOPES: readonly AccessScope[] = [
  "workspace",
  "specific-people",
  "owner-only",
];

/**
 * Whether the bulk-access dialog's Apply button should be enabled given the
 * picker's current onChange payload. `null` (no selection yet) and
 * `public_to` with zero invocation targets are not ready.
 */
export function isAccessChangeReady(
  change: { permission_mode: AgentPermissionMode; invocation_targets: readonly { target_type: string }[] } | null,
): boolean {
  if (!change) return false;
  if (change.permission_mode === "private") return true;
  return change.invocation_targets.length > 0;
}
