import type { Workspace } from "../types";

export function sortWorkspaces(workspaces: Workspace[]): Workspace[] {
  return [...workspaces].sort((a, b) => {
    if (!!a.pinned_at !== !!b.pinned_at) return a.pinned_at ? -1 : 1;
    if (a.workspace_position != null && b.workspace_position != null) {
      const position = a.workspace_position - b.workspace_position;
      if (position !== 0) return position;
    } else if (a.workspace_position != null) {
      return -1;
    } else if (b.workspace_position != null) {
      return 1;
    }
    if (a.pinned_at && b.pinned_at) {
      const pinned = b.pinned_at.localeCompare(a.pinned_at);
      if (pinned !== 0) return pinned;
    }
    return a.created_at.localeCompare(b.created_at);
  });
}
