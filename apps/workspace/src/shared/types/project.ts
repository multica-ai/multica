export type ProjectStatus = "planned" | "in_progress" | "paused" | "completed" | "cancelled";

export type ProjectLeadType = "member" | "agent";

export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  icon: string | null;
  status: ProjectStatus;
  lead_type: ProjectLeadType | null;
  lead_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateProjectRequest {
  title: string;
  description?: string;
  icon?: string;
  status?: ProjectStatus;
  lead_type?: ProjectLeadType;
  lead_id?: string;
}

export interface UpdateProjectRequest {
  title?: string;
  description?: string | null;
  icon?: string | null;
  status?: ProjectStatus;
  lead_type?: ProjectLeadType | null;
  lead_id?: string | null;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}