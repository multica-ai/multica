export interface GitlabMergeRequest {
  id: string;
  workspace_id: string;
  repo_owner: string;
  repo_name: string;
  mr_number: number;
  title: string;
  state: "opened" | "merged" | "closed";
  html_url: string;
  source_branch: string | null;
  target_branch: string | null;
  author_login: string | null;
  author_avatar_url: string | null;
  merged_at: string | null;
  closed_at: string | null;
  mr_created_at: string;
  mr_updated_at: string;
}

export interface GitlabSettings {
  /** Whether the deployment has GitLab App credentials configured. */
  configured: boolean;
  /** Master switch. When false, every UI affordance is gated off. */
  enabled: boolean;
  /** Whether the caller can manage the GitLab integration. */
  canManage: boolean;
  settings: {
    /** Issue-detail MR sidebar visibility. Implies `enabled`. */
    mrSidebarEnabled: boolean;
    /** Auto-link issues to MRs from webhook payloads. Implies `enabled`. */
    autoLinkEnabled: boolean;
  };
}
