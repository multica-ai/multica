import type { Workspace } from "../types";
import { useAuthStore } from "../auth";
import { paths } from "./paths";

/**
 * Priority:
 *   has workspace                         → /<first.slug>/issues
 *   zero workspaces && !hasOnboarded      → /onboarding
 *   zero workspaces && hasOnboarded       → /workspaces/new
 *
 * Workspace presence wins over onboarding state: a user invited into an
 * existing workspace must NOT be bounced into the new-workspace wizard
 * just because their personal `onboarded_at` is still null.
 */
export function resolvePostAuthDestination(
  workspaces: Workspace[],
  hasOnboarded: boolean,
): string {
  const first = workspaces[0];
  if (first) {
    return paths.workspace(first.slug).issues();
  }
  return hasOnboarded ? paths.newWorkspace() : paths.onboarding();
}

/**
 * Single source of truth: backed by `users.onboarded_at`, which
 * arrives with the user object on every auth response.
 */
export function useHasOnboarded(): boolean {
  return useAuthStore((s) => s.user?.onboarded_at != null);
}
