export type MemberRole = "owner" | "admin" | "member";

export interface WorkspaceRepo {
  url: string;
  description: string;
}

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  context: string | null;
  settings: Record<string, unknown>;
  repos: WorkspaceRepo[];
  issue_prefix: string;
  created_at: string;
  updated_at: string;
}

export interface Member {
  id: string;
  workspace_id: string;
  user_id: string;
  role: MemberRole;
  created_at: string;
}

export interface User {
  id: string;
  name: string;
  email: string;
  avatar_url: string | null;
  created_at: string;
  updated_at: string;
}

export interface MemberWithUser {
  id: string;
  workspace_id: string;
  user_id: string;
  role: MemberRole;
  created_at: string;
  name: string;
  email: string;
  avatar_url: string | null;
}


export interface AISettingsResponse {
  provider: string;
  api_key_masked: string;
  has_api_key: boolean;
  masked_api_key: string;
  base_url: string;
  model: string;
  label_rules: string[];
  env_key_present: boolean;
}

export interface UpdateAISettingsRequest {
  provider?: string;
  api_key?: string;
  base_url?: string;
  model?: string;
  label_rules?: string[];
}
