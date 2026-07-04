export interface IssueType {
  id: string;
  workspace_id: string;
  name: string;
  description: string | null;
  icon: string;
  color: string;
  is_default: boolean;
  position: number;
  created_at: string;
  updated_at: string;
}

export interface CreateIssueTypeRequest {
  name: string;
  description?: string;
  icon?: string;
  color?: string;
  is_default?: boolean;
  position?: number;
}

export interface UpdateIssueTypeRequest {
  name?: string;
  description?: string;
  icon?: string;
  color?: string;
  is_default?: boolean;
  position?: number;
}
