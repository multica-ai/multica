export interface Team {
  id: string;
  workspace_id: string;
  name: string;
  key: string;
  description: string;
  icon: string | null;
  issue_counter: number;
  is_default: boolean;
  archived_at: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateTeamRequest {
  name: string;
  key: string;
  description?: string;
  icon?: string | null;
}

export interface UpdateTeamRequest {
  name?: string;
  key?: string;
  description?: string;
  icon?: string | null;
}

export interface ListTeamsResponse {
  teams: Team[];
  total: number;
}
