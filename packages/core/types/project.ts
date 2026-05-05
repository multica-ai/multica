// CEREBRO-PATCH(core-types-project): cerebro modification of upstream file
export type ProjectStatus = "planned" | "in_progress" | "paused" | "completed" | "cancelled";

export type ProjectPriority = "urgent" | "high" | "medium" | "low" | "none";

export type ProjectAccess = "workspace" | "restricted";

export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  icon: string | null;
  color: string | null;
  repo_url: string | null;
  status: ProjectStatus;
  priority: ProjectPriority;
  lead_type: "member" | "agent" | null;
  lead_id: string | null;
  access: ProjectAccess;
  created_at: string;
  updated_at: string;
  issue_count: number;
  done_count: number;
}

export interface ProjectMember {
  user_id: string;
  name: string;
  email: string;
  avatar_url: string | null;
  added_at: string;
}

export interface CreateProjectRequest {
  title: string;
  description?: string;
  icon?: string;
  color?: string;
  repo_url?: string;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent";
  lead_id?: string;
}

export interface UpdateProjectRequest {
  title?: string;
  description?: string | null;
  icon?: string | null;
  color?: string | null;
  repo_url?: string | null;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent" | null;
  lead_id?: string | null;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}
