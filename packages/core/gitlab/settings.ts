import type { Workspace } from "../types";

export interface GitlabDerivedSettings {
  /** Master switch. When false, every UI affordance and side-effect is gated off. */
  enabled: boolean;
  /** Issue-detail MR sidebar visibility. Implies `enabled`. */
  mrSidebar: boolean;
  /** Auto-link issues to MRs from webhook payloads. Implies `enabled`. */
  autoLinkMRs: boolean;
}

/**
 * Pure derivation from a workspace's settings JSONB. Defaults every flag to
 * false so workspaces must explicitly opt in to GitLab features.
 */
export function deriveGitlabSettings(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): GitlabDerivedSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  const enabled = s.gitlab_enabled === true;
  return {
    enabled,
    mrSidebar: enabled && s.gitlab_mr_sidebar_enabled !== false,
    autoLinkMRs: enabled && s.gitlab_auto_link_enabled !== false,
  };
}
