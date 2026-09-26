import type { Workspace } from "../types";

export interface GitHubSettings {
  /** Master switch. When false, every UI affordance and side-effect is gated off. */
  enabled: boolean;
  /** Issue-detail PR sidebar visibility. Implies `enabled`. */
  prSidebar: boolean;
  /** Co-authored-by trailer in agent commits. Implies `enabled`. */
  coAuthor: boolean;
  /** Auto-link issues ↔ PRs from webhook payloads. Implies `enabled`. */
  autoLinkPRs: boolean;
}

/**
 * Pure derivation from a workspace's settings JSONB. Defaults every flag to
 * true so workspaces predating MUL-2414 keep the historical "all on" behavior.
 */
export function deriveGitHubSettings(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): GitHubSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  const enabled = s.github_enabled !== false;
  return {
    enabled,
    prSidebar: enabled && s.github_pr_sidebar_enabled !== false,
    coAuthor: enabled && s.co_authored_by_enabled !== false,
    autoLinkPRs: enabled && s.github_auto_link_prs_enabled !== false,
  };
}

/** The `pr_merge_status` value that leaves an issue's status alone. */
export const PR_MERGE_STATUS_NONE = "none";

/**
 * What merging every PR linked to an issue does (MUL-7726): `"none"`, or the
 * key of the status the issue moves to. Without the key it follows the retired
 * `pr_auto_complete_enabled` switch (off → none, else Done), as the server does.
 * Not GitHub-specific — self-hosted providers follow the same setting — so it
 * ignores the GitHub master switch. The server re-validates the key against
 * the status catalog.
 */
export function derivePRMergeStatus(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): string {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  const value = s.pr_merge_status;
  if (value === undefined || value === null) {
    const legacy = s.pr_auto_complete_enabled;
    return legacy === undefined || legacy === null || legacy === true ? "done" : PR_MERGE_STATUS_NONE;
  }
  if (typeof value !== "string") return PR_MERGE_STATUS_NONE;
  return value.trim().toLowerCase() || PR_MERGE_STATUS_NONE;
}
