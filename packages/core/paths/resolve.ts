import type { Workspace } from "../types";
import { useAuthStore } from "../auth";
import { paths } from "./paths";

/**
 * Priority:
 *   workspace[0] → /<first.slug>/issues
 *   no workspace → /workspaces/new
 *
 * The onboarding guide has been removed; all authenticated users land
 * directly in their first workspace or the workspace creation flow.
 * `onboarded_at` is still tracked server-side for analytics and future
 * gating, but it no longer drives routing.
 */
export function resolvePostAuthDestination(
  workspaces: Workspace[],
  hasOnboarded: boolean,
): string {
  // All authenticated users go to their first workspace or workspace creation.
  void hasOnboarded;
  const first = workspaces[0];
  if (first) {
    return paths.workspace(first.slug).issues();
  }
  return paths.newWorkspace();
}

/**
 * Single source of truth: backed by `users.onboarded_at`, which
 * arrives with the user object on every auth response.
 */
export function useHasOnboarded(): boolean {
  return useAuthStore((s) => s.user?.onboarded_at != null);
}
