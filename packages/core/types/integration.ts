export type IntegrationProvider = "redmine";

export interface WorkspaceIntegration {
  id: string;
  workspace_id: string;
  provider: IntegrationProvider;
  instance_url: string;
  created_at: string;
  updated_at: string;
}

export interface UserIntegrationCredential {
  provider: IntegrationProvider;
  has_key: boolean;
}

export interface ProjectIntegrationLink {
  id: string;
  project_id: string;
  provider: IntegrationProvider;
  external_project_id: string;
  external_project_name: string | null;
  created_at: string;
}

export interface IssueIntegrationLink {
  id: string;
  issue_id: string;
  provider: IntegrationProvider;
  external_issue_id: string;
  external_issue_url: string | null;
  external_issue_title: string | null;
  created_at: string;
}

export interface RedmineProject {
  id: number;
  name: string;
  identifier: string;
  description: string;
}

export interface RedmineIssue {
  id: number;
  subject: string;
  description: string;
  project: { id: number; name: string };
}
