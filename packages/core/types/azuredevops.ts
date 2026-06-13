export type ADOPullRequestState = "open" | "draft" | "merged" | "abandoned";

/** Aggregated gate result from ADO PR policies + build checks. */
export type ADOPolicyStatus = "approved" | "blocked" | "pending";

/** Build check conclusion parallel to GitHub's checks_conclusion. */
export type ADOChecksConclusion = "passed" | "failed" | "pending";

export interface ADOInstallation {
  id: string;
  workspace_id: string;
  org_url: string;
  display_name: string;
  /** Only present in the create response so the operator can copy it once. */
  webhook_url?: string;
  created_at: string;
}

export interface ADOPullRequest {
  id: string;
  workspace_id: string;
  /** Always "azure_devops" — discriminator so mixed PR lists can be typed. */
  provider: "azure_devops";
  org_url: string;
  project: string;
  repo_name: string;
  number: number;
  title: string;
  state: ADOPullRequestState;
  html_url: string;
  branch: string | null;
  author_login: string | null;
  author_avatar_url: string | null;
  merged_at: string | null;
  closed_at: string | null;
  pr_created_at: string;
  pr_updated_at: string;
  /** Aggregated gate result: approved / blocked / pending, or null when unknown. */
  policy_status: ADOPolicyStatus | null;
  merge_status: string | null;
  checks_passed: number;
  checks_failed: number;
  checks_pending: number;
  checks_conclusion: ADOChecksConclusion | null;
}

export interface ListADOInstallationsResponse {
  installations: ADOInstallation[];
  /** Whether the caller can connect / disconnect installations. */
  can_manage: boolean;
}
