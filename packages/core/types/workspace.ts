// CEREBRO-PATCH(core-types-workspace): cerebro modification of upstream file
export type MemberRole = "owner" | "admin" | "member";

export interface WorkspaceRepo {
  url: string;
}

export interface FirtalGatewayWorkspaceSettings {
  enabled?: boolean;
  gateway_url?: string;
  api_key_configured?: boolean;
  model?: string;
}

export interface WorkspaceSettings extends Record<string, unknown> {
  firtal_gateway?: FirtalGatewayWorkspaceSettings;
}

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  context: string | null;
  settings: WorkspaceSettings;
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
  preferences: Record<string, unknown>;
  onboarded_at: string | null;
  /**
   * JSONB payload from the server. Typed as `unknown` here so this module
   * stays independent of the questionnaire shape — the onboarding views
   * cast into `Partial<QuestionnaireAnswers>` when reading. Server always
   * returns an object (defaults to `{}`), never null.
   */
  onboarding_questionnaire: Record<string, unknown>;
  /**
   * Terminal state for the post-onboarding "import starter content" prompt.
   *   null             → new user, dialog will show on issues-list landing
   *   'imported'       → accepted, starter project + issues were seeded
   *   'dismissed'      → declined, never ask again
   *   'skipped_legacy' → backfilled for users who finished onboarding
   *                      before this feature shipped
   * Kept as a generic `string | null` here so future states (e.g.
   * 'retry_after_error') can be added without churning this type.
   */
  starter_content_state: string | null;
  /** Preferred UI language. null means "follow client/system". */
  language: string | null;
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
  budget_enforcement_enabled: boolean;
}

export interface MemberUsage {
  user_id: string;
  daily_cents: number;
  monthly_cents: number;
  daily_window: string;
  monthly_window: string;
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
