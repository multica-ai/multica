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
 * Pure derivation from a workspace's settings JSONB. The master switch and the
 * PR-sidebar / auto-link sub-flags default to true so workspaces predating
 * MUL-2414 keep the historical "all on" behavior. The Co-authored-by trailer is
 * the exception: it is opt-in and defaults to OFF, surfacing as enabled only
 * when the workspace explicitly sets `co_authored_by_enabled` to true.
 */
export function deriveGitHubSettings(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): GitHubSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  const enabled = s.github_enabled !== false;
  return {
    enabled,
    prSidebar: enabled && s.github_pr_sidebar_enabled !== false,
    coAuthor: enabled && s.co_authored_by_enabled === true,
    autoLinkPRs: enabled && s.github_auto_link_prs_enabled !== false,
  };
}
