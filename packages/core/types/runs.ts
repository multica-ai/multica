export interface WorkspaceTaskRun {
  id: string;
  agent_id: string;
  agent_name: string;
  issue_id?: string;
  issue_identifier?: string;
  issue_title?: string;
  status: string;
  error: string | null;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
}

export interface ListWorkspaceTaskRunsResponse {
  items: WorkspaceTaskRun[];
  total: number;
  has_more: boolean;
  limit: number;
  offset: number;
}
