import type { Workspace } from "../types";

/** Missing or non-false settings preserve the existing @all behavior. */
export function isMentionAllEnabled(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): boolean {
  return workspace?.settings?.mention_all_enabled !== false;
}
