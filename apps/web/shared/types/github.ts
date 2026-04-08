export type PullRequestStatus = "open" | "draft" | "merged" | "closed";

export interface PullRequest {
  id: string;
  issue_id: string;
  repo_owner: string;
  repo_name: string;
  pr_number: number;
  title: string;
  status: PullRequestStatus;
  author: string;
  url: string;
  branch: string | null;
  created_at: string;
  updated_at: string;
}

export interface GitHubInstallation {
  id: string;
  workspace_id: string;
  installation_id: number;
  account_login: string;
  account_type: string;
  created_at: string;
}
