export type ProjectStatus =
  | "backlog"
  | "planned"
  | "in_progress"
  | "completed"
  | "cancelled";

export type ProjectLeadType = "member" | "agent";

export interface ProjectProgress {
  total: number;
  completed: number;
  percent: number;
}

export interface Project {
  id: string;
  workspace_id: string;
  name: string;
  description: string | null;
  status: ProjectStatus;
  icon: string | null;
  color: string | null;
  lead_type: ProjectLeadType | null;
  lead_id: string | null;
  start_date: string | null;
  target_date: string | null;
  sort_order: number;
  progress?: ProjectProgress;
  created_at: string;
  updated_at: string;
}

export interface CreateProjectRequest {
  name: string;
  description?: string;
  status?: ProjectStatus;
  icon?: string;
  color?: string;
  lead_type?: ProjectLeadType;
  lead_id?: string;
  start_date?: string;
  target_date?: string;
}

export interface UpdateProjectRequest {
  name?: string;
  description?: string;
  status?: ProjectStatus;
  icon?: string;
  color?: string;
  lead_type?: ProjectLeadType | null;
  lead_id?: string | null;
  start_date?: string | null;
  target_date?: string | null;
  sort_order?: number;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}
