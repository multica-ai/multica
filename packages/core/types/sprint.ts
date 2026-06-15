export type SprintState = "planning" | "active" | "completed";

export interface Sprint {
  id: string;
  workspace_id: string;
  project_id: string;
  name: string;
  goal: string | null;
  start_date: string | null;
  end_date: string | null;
  state: SprintState;
  created_at: string;
  updated_at: string;
}

export interface SprintIssue {
  id: string;
  workspace_id: string;
  project_id: string | null;
  number: number;
  title: string;
  status: string;
  priority: string;
  assignee_id: string | null;
  sprint_id: string | null;
  estimate: number | null;
  created_at: string;
}

export interface VelocityPoint {
  sprint_id: string;
  sprint_name: string;
  start_date: string | null;
  end_date: string | null;
  completed_points: number;
  total_points: number;
}

export interface BurndownIssue {
  id: string;
  status: string;
  estimate: number | null;
  updated_at: string;
}

export interface ListSprintsResponse {
  sprints: Sprint[];
}

export interface ListSprintIssuesResponse {
  issues: SprintIssue[];
}

export interface ListBacklogResponse {
  issues: SprintIssue[];
}

export interface CreateSprintRequest {
  name: string;
  goal?: string | null;
  start_date?: string | null;
  end_date?: string | null;
}

export interface UpdateSprintRequest {
  name?: string;
  goal?: string | null;
  start_date?: string | null;
  end_date?: string | null;
}

export interface CompleteSprintRequest {
  carry_to: string; // "backlog" | sprint_id
}

export interface SprintVelocityResponse {
  velocity: number;
}

export interface ProjectVelocityResponse {
  velocity: VelocityPoint[];
}

export interface SprintBurndownResponse {
  issues: BurndownIssue[];
}
