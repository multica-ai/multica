export type MemberRole = "owner" | "admin" | "member";

export interface WorkspaceRepo {
  url: string;
  description?: string;
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
  avatar_url: string | null;
  created_at: string;
  updated_at: string;
}

/**
 * One MCP server in the workspace's library.
 *
 * This is the WHOLE read shape: the stored configuration is write-only, so
 * urls, commands, headers, and env never leave the server for any role. UI
 * that needs to change an entry sends a replacement rather than editing what
 * it read back.
 *
 * `enabled` is only present on an AGENT's assignment list, where it is the
 * per-agent toggle; the workspace library listing has no binding to report.
 */
export interface WorkspaceMcpServer {
  id: string;
  workspace_id: string;
  name: string;
  transport: string;
  enabled?: boolean;
  /**
   * Live agents the entry is assigned to. Only the workspace library listing
   * carries it, and servers older than this field omit it.
   */
  agent_count?: number;
  created_at: string;
  updated_at: string;
}

export type CodeGraphRepositoryStatus = "pending" | "processing" | "ready" | "failed";

export interface CodeGraphRepository {
 auth_mode?:"token"|"anonymous";
 credential_id?:string;
  progress?: { id: string; status: string; phase: string; cancel_requested: boolean; created_at: string; started_at?: string; finished_at?: string; phase_started_at: string; error?: string };
	builds?: { id: string; status: string; commit: string; created_at: string; error?: string }[];
	active_build_id?: string;
	public_read_enabled?: boolean;
	symbol_count?: number;
	edge_count?: number;
  agents?: { id: string; name: string; created_at: string }[];
  id: string;
  workspace_id: string;
  repo_url: string;
  repo_name: string;
  branch: string;
  status: CodeGraphRepositoryStatus;
  internal_status?: string;
  commit_hash?: string;
  file_count: number;
  sync_error?: string;
  last_indexed_at?: string;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface CodeGraphStats {
  total: number;
  ready: number;
  processing: number;
  pending: number;
  failed: number;
  files: number;
}

export interface CodeGraphRepositoryListResponse {
  items: CodeGraphRepository[];
  stats: CodeGraphStats;
}

export interface CodeGraphRepositoryAssignment {
  repository_id: string;
  agent_id: string;
  created_at: string;
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
  onboarded_at: string | null;
  /**
   * JSONB payload from the server. Typed as `unknown` here so this module
   * stays independent of the questionnaire shape — the onboarding views
   * cast into `Partial<QuestionnaireAnswers>` when reading. Server always
   * returns an object (defaults to `{}`), never null.
   */
  onboarding_questionnaire: Record<string, unknown>;
  /** Preferred UI language. null means "follow client/system". */
  language: string | null;
  /**
   * Free-form self-description (role, stack, preferences). Injected into
   * the agent brief so coding agents have cheap, durable context about
   * who is requesting the work. Server always returns a string —
   * NOT NULL DEFAULT '' at the column level, empty when unset.
   */
  profile_description: string;
  /** Pinned IANA tz; null means "use browser-detected tz at render time". */
  timezone: string | null;
  /**
   * Newest release whose What's new highlights the user has dismissed.
   * null = never dismissed; undefined = the server predates read tracking,
   * so the client must not auto-open the card (it would reappear forever).
   */
  whats_new_seen_version?: string | null;
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

export interface Invitation {
  id: string;
  workspace_id: string;
  inviter_id: string;
  invitee_email: string;
  invitee_user_id: string | null;
  role: MemberRole;
  status: "pending" | "accepted" | "declined" | "expired";
  created_at: string;
  updated_at: string;
  expires_at: string;
  inviter_name?: string;
  inviter_email?: string;
  workspace_name?: string;
}

export interface ShareLink {
  id: string;
  workspace_id: string;
  code: string;
  created_by: string;
  role: MemberRole;
  expires_at: string | null;
  max_uses: number | null;
  use_count: number;
  is_active: boolean;
  created_at: string;
  creator_name?: string;
  creator_email?: string;
}

export interface ShareLinkInfo {
  workspace_name: string;
  workspace_slug: string;
  creator_name?: string;
  role: MemberRole;
}
