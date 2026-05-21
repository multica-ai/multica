export type SquadMemberType = "agent" | "member";

export type SquadActivityOutcome = "action" | "no_action" | "failed";

export interface Squad {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  instructions: string;
  avatar_url: string | null;
  leader_id: string;
  creator_id: string;
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  archived_by: string | null;
}

export interface SquadMember {
  id: string;
  squad_id: string;
  member_type: SquadMemberType;
  member_id: string;
  role: string;
  created_at: string;
}

export interface SquadActivityLog {
  id: string;
  squad_id: string;
  issue_id: string;
  trigger_comment_id: string | null;
  leader_id: string;
  outcome: SquadActivityOutcome;
  details: unknown;
  created_at: string;
}

export interface CreateSquadRequest {
  name: string;
  description?: string;
  leader_id: string;
  avatar_url?: string; // CEREBRO-PATCH(upstream-create-squad-avatar): JEH-1541 align create request type with backend.
}

export interface UpdateSquadRequest {
  name?: string;
  description?: string;
  instructions?: string;
  leader_id?: string;
  avatar_url?: string;
}

export interface AddSquadMemberRequest {
  member_type: SquadMemberType;
  member_id: string;
  role?: string;
}

export interface RemoveSquadMemberRequest {
  member_type: SquadMemberType;
  member_id: string;
}

export interface UpdateSquadMemberRoleRequest {
  member_type: SquadMemberType;
  member_id: string;
  role: string;
}

export interface CreateSquadActivityLogRequest {
  squad_id: string;
  issue_id: string;
  trigger_comment_id?: string;
  outcome: SquadActivityOutcome;
  details?: unknown;
}

export type SquadMemberStatusValue = "working" | "idle" | "offline" | "unstable";

export interface SquadActiveIssueBrief {
  issue_id: string;
  identifier: string;
  title: string;
  status: string;
  priority: string;
}

export interface SquadMemberStatus {
  member_type: SquadMemberType;
  member_id: string;
  name: string;
  avatar_url: string | null;
  role: string;
  status: SquadMemberStatusValue | string | null;
  active_issue: SquadActiveIssueBrief | null;
  updated_at: string | null;
}

export interface SquadMemberStatusListResponse {
  members: SquadMemberStatus[];
}
