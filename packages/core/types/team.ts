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
  /** Requesting user's membership view — the sidebar shows only joined
   *  teams, ordered by sort_order (per-user fractional position). */
  is_member: boolean;
  sort_order: number;
}

export interface CreateTeamRequest {
  name: string;
  key: string;
  description?: string;
  icon?: string | null;
  /** Workspace members invited alongside the creator (who joins as lead). */
  member_ids?: string[];
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

/** Caller's own membership row, as returned by PATCH /api/teams/{id}/membership. */
export interface TeamMembership {
  team_id: string;
  sort_order: number;
}

/** A team member with user display data (GET /api/teams/{id}/members). */
export interface TeamMember {
  user_id: string;
  name: string;
  email: string;
  avatar_url: string | null;
  /** "lead" | "member" — informational in v1, no privileges attached. */
  role: string;
  created_at: string;
}

export interface ListTeamMembersResponse {
  members: TeamMember[];
  total: number;
}
