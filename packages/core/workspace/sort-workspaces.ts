import type { Workspace } from "../types";

export function sortWorkspaces(workspaces: Workspace[]): Workspace[] {
  return [...workspaces].sort((a, b) => {
    if (a.pinned_at && b.pinned_at) return b.pinned_at.localeCompare(a.pinned_at);
    if (a.pinned_at) return -1;
    if (b.pinned_at) return 1;
    return a.created_at.localeCompare(b.created_at);
  });
}
