import type { Workspace } from "../types";
import { paths } from "./paths";

/**
 * Pick a concrete post-auth destination from projected workspaces. Workspace
 * admission is decided by the VIBES session/membership handoff, never by the
 * historical Multica `onboarded_at` field.
 *
 */
export function resolvePostAuthDestination(workspaces: Workspace[]): string {
  const first = workspaces[0];
  if (first) {
    return paths.workspace(first.slug).issues();
  }
  return paths.newWorkspace();
}
