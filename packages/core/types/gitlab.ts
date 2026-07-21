export type GitLabMRState = "open" | "closed" | "merged" | "locked";

export interface GitLabConnection {
  id: string;
  workspace_id: string;
  namespace: string;
  namespace_type: "group" | "user";
  avatar_url: string | null;
  created_at: string;
  /** Per-connection X-Gitlab-Token value; only present for owners/admins. */
  webhook_secret?: string | null;
}

export interface GitLabMergeRequest {
  id: string;
  workspace_id: string;
  project_path: string;
  mr_iid: number;
  title: string;
  state: GitLabMRState;
  html_url: string;
  source_branch: string | null;
  author_username: string | null;
  author_avatar_url: string | null;
  merged_at: string | null;
  closed_at: string | null;
  mr_created_at: string;
  mr_updated_at: string;
}

export interface ListGitLabConnectionsResponse {
  connections: GitLabConnection[];
  configured: boolean;
  can_manage?: boolean;
}

export interface GitLabIssue {
  gl_issue_iid: number;
  project_path: string;
  url: string;
  gl_assignee_username: string | null;
}
