import type { IssueStatus } from "./issue";

export interface WorkspaceColumnConfig {
  id: string;
  workspace_id: string;
  status: IssueStatus;
  instructions: string;
  allowed_transitions: IssueStatus[];
  created_at: string;
  updated_at: string;
}

export interface UpdateWorkspaceColumnConfigRequest {
  instructions: string;
  allowed_transitions: IssueStatus[];
}
