import type { TaskFailureReason } from "../types";

/**
 * Picks the copy key a UI surface should render for a server-supplied
 * `failure_reason`, degrading instead of falling off a cliff.
 *
 * Resolution order:
 *   1. Exact match — the surface has copy for this precise reason.
 *   2. Coarse family — `agent_error.provider_network` degrades to
 *      `agent_error` when the surface only has family-level copy.
 *   3. `undefined` — nothing usable; the caller renders its own fallback.
 *
 * Why this exists: the backend's taxonomy is refined (`agent_error.*`) and
 * grows independently of shipped clients. Before MUL-5370 the surfaces did an
 * exact `Record` lookup against the six-value coarse enum, so every refined
 * reason missed and rendered a generic "something went wrong" — the useful
 * classification the backend had already computed was thrown away at the last
 * step. Step 2 makes an unseen `agent_error.<new_thing>` land on the family
 * line rather than the fallback, which is the repo's "enum drift downgrades,
 * not crashes" rule applied to copy.
 *
 * Pure and dependency-free so mobile can import it directly.
 */
export function resolveFailureReasonKey<K extends string>(
  reason: TaskFailureReason | string | null | undefined,
  copy: Readonly<Partial<Record<K, unknown>>>,
): K | undefined {
  if (!reason) return undefined;
  if (Object.prototype.hasOwnProperty.call(copy, reason)) {
    return reason as K;
  }
  const separator = reason.indexOf(".");
  if (separator > 0) {
    const family = reason.slice(0, separator);
    if (Object.prototype.hasOwnProperty.call(copy, family)) {
      return family as K;
    }
  }
  return undefined;
}
