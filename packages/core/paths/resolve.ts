import type { Workspace } from "../types";
import { useAuthStore } from "../auth";
import { paths } from "./paths";

/**
 * Priority (onboarded-first):
 *   !hasOnboarded               → /workspaces/new (skip onboarding guide)
 *   hasOnboarded + workspace[0] → /<first.slug>/issues
 *   hasOnboarded + no workspace → /workspaces/new
 *
 * Onboarding questionnaire and runtime-picker steps are skipped for new
 * users — they land directly on workspace creation. The onboarding flag
 * is still tracked (`onboarded_at`) for analytics and future gating, but
 * the guided UI flow is no longer shown.
 */
export function resolvePostAuthDestination(
  workspaces: Workspace[],
  hasOnboarded: boolean,
): string {
  // Onboarding guide is skipped — all users go to their first workspace
  // or workspace creation regardless of onboarded status.
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
