import type { Workspace } from "../types";
import { useAuthStore } from "../auth";
import { paths } from "./paths";

/**
 * Priority:
 *   !hasOnboarded                         → /onboarding
 *   hasOnboarded && has workspace         → /<first.slug>/issues
 *   hasOnboarded && zero workspaces       → /workspaces/new
 */
export function resolvePostAuthDestination(
  workspaces: Workspace[],
  hasOnboarded: boolean,
): string {
  if (!hasOnboarded) {
    return paths.onboarding();
  }
  const first = workspaces[0];
  return first ? paths.workspace(first.slug).issues() : paths.newWorkspace();
}

/**
 * Single source of truth: backed by `users.onboarded_at`, which
 * arrives with the user object on every auth response.
 */
export function useHasOnboarded(): boolean {
  return useAuthStore((s) => s.user?.onboarded_at != null);
}
