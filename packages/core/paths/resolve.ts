import type { Workspace } from "../types";
import { useAuthStore } from "../auth";
import { paths } from "./paths";

/**
 * Priority:
 *   has workspace                         → /<first.slug>/issues
 *   zero workspaces                       → /workspaces/new
 *
 * First-run onboarding is intentionally bypassed. New users go straight to
 * workspace creation, and users with an existing workspace go straight to
 * their first workspace.
 *
 * Callers that need invitation-aware routing (callback / login) handle the
 * "un-onboarded with pending invites" branch themselves before calling
 * this resolver — this resolver only deals with the post-invite-check
 * destination.
 */
export function resolvePostAuthDestination(
  workspaces: Workspace[],
  _hasOnboarded: boolean,
): string {
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
