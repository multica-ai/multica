export interface Space {
  id: string;
  workspace_id: string;
  name: string;
  key: string;
  icon: string | null;
  issue_counter: number;
  /** Stable workspace-level fallback for context-free creation. */
  is_default: boolean;
  visibility: "open" | "private";
  archived_at: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  /** Requesting user's membership view — the sidebar shows only joined
   *  spaces, ordered by sort_order (per-user fractional position). */
  is_member: boolean;
  member_role: "lead" | "admin" | "member" | "guest" | null;
  /** Personal navigation shortcut; never grants Space access. */
  is_pinned: boolean;
  /** Personal notification subscription; never grants Space access. */
  is_followed: boolean;
  sort_order: number;
}

export interface CreateSpaceRequest {
  name: string;
  key: string;
  icon?: string | null;
  visibility?: "open" | "private";
  /** Workspace members invited alongside the creator (who joins as lead). */
  member_ids?: string[];
}

export interface UpdateSpaceRequest {
  name?: string;
  key?: string;
  icon?: string | null;
  visibility?: "open" | "private";
}

export interface ListSpacesResponse {
  spaces: Space[];
  total: number;
}

export interface RestoreSpaceResponse {
  space: Space;
  paused_autopilot_count: number;
}

export interface ResumeSpaceAutopilotsResponse {
  resumed_autopilot_count: number;
}

/** Caller's own membership row, as returned by PATCH /api/spaces/{id}/membership. */
export interface SpaceMembership {
  space_id: string;
  sort_order: number;
}

export interface SpacePreference {
  space_id: string;
  is_pinned: boolean;
  is_followed: boolean;
  sort_order: number;
}

export interface UpdateSpacePreferenceRequest {
  is_pinned?: boolean;
  is_followed?: boolean;
  sort_order?: number;
}

export interface SpaceMemberRoleUpdate {
  space_id: string;
  user_id: string;
  role: "lead" | "admin" | "member" | "guest";
}

/** A space member with user display data (GET /api/spaces/{id}/members). */
export interface SpaceMember {
  user_id: string;
  name: string;
  email: string;
  avatar_url: string | null;
  /** Space-local collaboration role. */
  role: "lead" | "admin" | "member" | "guest";
  created_at: string;
}

export interface ListSpaceMembersResponse {
  members: SpaceMember[];
  total: number;
}
