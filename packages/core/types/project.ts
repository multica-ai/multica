export type ProjectStatus = "planned" | "in_progress" | "paused" | "completed" | "cancelled";

export type ProjectPriority = "urgent" | "high" | "medium" | "low" | "none";

export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  icon: string | null;
  status: ProjectStatus;
  priority: ProjectPriority;
  lead_type: "member" | "agent" | null;
  lead_id: string | null;
  created_at: string;
  updated_at: string;
  issue_count: number;
  done_count: number;
  /** IDs of workspace.repos entries linked to this project. Order is significant: the first id is the agent's default start directory. */
  repo_ids: string[];
}

export interface CreateProjectRequest {
  title: string;
  description?: string;
  icon?: string;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent";
  lead_id?: string;
  repo_ids?: string[];
}

export interface UpdateProjectRequest {
  title?: string;
  description?: string | null;
  icon?: string | null;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent" | null;
  lead_id?: string | null;
  /** Replace the project's repo links with this list. Passing an empty array clears all links. */
  repo_ids?: string[];
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}
