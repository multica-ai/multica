import type { A2aInvocationMode } from "../types";

/**
 * The four effective A2A invocation-scope states shown in the agents list,
 * derived from the authoritative `a2a_invocation_mode` (+ the
 * `specific_agents` whitelist) — the A2A-axis counterpart of
 * `effectiveAccessScope` (NEX-24). The axis is orthogonal to the
 * member-visibility scope: it ONLY governs agent callers.
 *
 *   - default           = not enabled (status-quo fail-closed)
 *   - any_agent         = every agent principal may invoke
 *   - squad_leaders     = only squad-leader agents may invoke
 *   - specific_agents   = only the `a2a_invocation_grants` whitelist
 */
export type A2aInvocationScope =
  | "disabled"
  | "any_agent"
  | "squad_leaders"
  | "specific_agents";

/**
 * Derive the effective A2A invocation scope from an agent's A2A fields. Fails
 * safe to "disabled" when the mode is absent (older backends / stale caches
 * omit the field) or unrecognised — mirroring the backend's forward-compat
 * rule that any unknown value is treated as `default` (fail closed, never
 * widen).
 */
export function effectiveA2aInvocationScope(
  mode: A2aInvocationMode | undefined | null,
): A2aInvocationScope {
  if (mode === "any_agent") return "any_agent";
  if (mode === "squad_leaders") return "squad_leaders";
  if (mode === "specific_agents") return "specific_agents";
  return "disabled";
}

/** All possible effective A2A scope values, in display order. */
export const ALL_A2A_INVOCATION_SCOPES: readonly A2aInvocationScope[] = [
  "any_agent",
  "squad_leaders",
  "specific_agents",
  "disabled",
];

/** Whitelist count backing the `specific_agents` mode (0 for every other
 *  mode / absent grants). */
export function a2aGrantCount(
  mode: A2aInvocationMode | undefined | null,
  grants: readonly string[] | undefined | null,
): number {
  if (mode !== "specific_agents") return 0;
  return grants?.length ?? 0;
}
