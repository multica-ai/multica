import type { Workspace } from "../types";
import { useAuthStore } from "../auth";
import { paths } from "./paths";

/**
 * Pick a concrete post-auth destination from projected workspaces. Workspace
 * admission is decided by the VIBES session/membership handoff, never by the
 * historical Multica `onboarded_at` field.
 *
 */
export function resolvePostAuthDestination(
  workspaces: Workspace[],
  _hasOnboarded?: boolean,
): string {
  const first = workspaces[0];
  if (first) {
    return paths.workspace(first.slug).issues();
  }
  return paths.newWorkspace();
}

/** Historical field retained for Settings and realtime callers outside #297. */
export function useHasOnboarded(): boolean {
  return useAuthStore((s) => s.user?.onboarded_at != null);
}
