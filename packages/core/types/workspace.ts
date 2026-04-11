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

export type SandboxProvider = "e2b" | "daytona";

export interface SandboxConfig {
  workspace_id: string;
  provider: SandboxProvider;
  provider_api_key: string; // redacted on GET (e.g. "****abcd")
  ai_gateway_api_key: string | null;
  git_pat: string | null;
  template_id: string | null;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface UpsertSandboxConfigRequest {
  provider: SandboxProvider;
  provider_api_key: string;
  ai_gateway_api_key?: string;
  git_pat?: string;
  template_id?: string;
  metadata?: Record<string, unknown>;
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
