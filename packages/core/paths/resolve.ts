import type { Workspace } from "../types";
import { useAuthStore } from "../auth";
import { paths } from "./paths";

/**
 * Priority:
 *   !hasOnboarded                         → /onboarding
 *   hasOnboarded && has workspace         → /<first.slug>/issues
 *   hasOnboarded && zero workspaces       → /workspaces/new
 *
 * `onboarded_at` is the single source of truth for whether the user has
 * passed first-contact. Backend transactions (CreateWorkspace,
 * AcceptInvitation) atomically set this field whenever a user joins a
 * `member` row, so "has workspace but !onboarded" is now a
 * physically impossible state — see migration 065 for the existing-data
 * backfill that closed the door retroactively.
 *
 * Callers that need invitation-aware routing (callback / login) handle the
 * "un-onboarded with pending invites" branch themselves before calling
 * this resolver — this resolver only deals with the post-invite-check
 * destination.
 */
export function resolvePostAuthDestination(
  workspaces: Workspace[],
  hasOnboarded: boolean,
): string {
  if (!hasOnboarded) {
    return paths.onboarding();
  }
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
