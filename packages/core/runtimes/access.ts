import type { RuntimeDevice } from "../types";

/**
 * Whether this member may run an agent on `runtime`.
 *
 * A private runtime is usable only by its owner; a public one by anyone in the
 * workspace. An unknown viewer (no session yet) is treated as allowed so a
 * still-loading auth state never hides a runtime the user does own — every
 * write path re-checks server-side, where admin overrides also apply.
 */
export function isRuntimeUsableForUser(
  runtime: RuntimeDevice,
  currentUserId: string | null,
): boolean {
  if (!currentUserId) return true;
  if (runtime.owner_id === currentUserId) return true;
  return runtime.visibility === "public";
}
