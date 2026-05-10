export type MemberRole = "owner" | "admin" | "member";

export interface WorkspaceRepo {
  url: string;
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
  /**
   * Optional pointer to the workspace's orchestrator agent. When set, the
   * server enqueues a task for this agent on every agent-authored issue
   * comment (skipping self-loops and the case where the orchestrator is
   * already the assignee). The orchestrator's persona decides what to do —
   * typically acknowledge, reassign, change status, or notify a human.
   */
  orchestrator_agent_id: string | null;
  created_at: string;
  updated_at: string;
  /** Whether the multi-participant Channels feature is exposed in this workspace.
   * Defaults to false until an admin opts in via Settings. */
  channels_enabled: boolean;
  /** Workspace-level retention default for channel messages, in days.
   * null = retain forever; per-channel overrides take precedence. */
  channel_retention_days: number | null;
  /** Whether the Ship Hub feature (PR Kanban + deploy strip) is exposed in
   * this workspace. Defaults to false until an admin opts in via Settings.
   * When false, the sidebar entry hides and every /api/ship/* endpoint
   * returns 404. */
  ship_hub_enabled: boolean;
  /** Whether a GitHub PAT is configured for this workspace. The token itself
   * is never returned by the API — only this presence flag — so the UI can
   * render "Configured" / "Not configured" without ever holding the secret. */
  github_token_set: boolean;
  /** Public URL the workspace owner pastes into GitHub's webhook config.
   * Computed server-side from MULTICA_API_BASE_URL so the frontend doesn't
   * have to thread the env var through. Empty string when the server hasn't
   * been configured (older builds). */
  ship_hub_webhook_url: string;
  /** Mirrors `github_token_set`: true when a webhook secret has been
   * configured. The plaintext value is only ever returned by
   * POST /workspaces/{id}/ship_hub/regenerate_webhook_secret. */
  ship_hub_webhook_secret_set: boolean;
  /** True when a smoke-test GitHub Actions workflow filename has been
   * configured for the workspace. Drives the release page's "Run smoke
   * tests" button — when false, the button hides (it would 400 anyway)
   * and the smoke pill renders "Not configured" instead of empty. */
  ship_hub_smoke_workflow_set: boolean;
  /**
   * Auto-detect deploys via GitHub Actions polling. When true, the
   * server polls completed runs of the configured workflow on `main`
   * every 2 minutes and auto-links matching releases by sha — no
   * manual "Mark deploy as landed" click required. The release page's
   * "awaiting deploy" copy is swapped to mention the polling cadence
   * when these are set, so the user knows the link is being watched
   * rather than wondering if the feature is broken.
   *
   * Optional in the type (drift safety) — older Electron builds
   * fetching a fresh server response see the field; older servers
   * returning a stale row without the column degrade to the
   * pre-poller copy.
   */
  ship_hub_deploy_workflow_staging_set?: boolean;
  ship_hub_deploy_workflow_production_set?: boolean;
  /**
   * Per-risk-tier approval rule (Phase 7d follow-up — configurable
   * approvals). One of `"member" | "admin" | "approver" | "two"`.
   *
   * Optional in the type so older Electron builds that fetch a fresh
   * server response (which always includes the field) still typecheck
   * against a cached older shape, AND so a server returning a stale
   * row without the column degrades gracefully. The runtime gates
   * fall back to legacy hardcoded behavior (low/medium → "member",
   * high → "approver", critical → "two") when the field is missing
   * or holds an unrecognized value.
   */
  ship_hub_approval_low?: ApprovalRule;
  ship_hub_approval_medium?: ApprovalRule;
  ship_hub_approval_high?: ApprovalRule;
  ship_hub_approval_critical?: ApprovalRule;
  /** When false, separation-of-duties is enforced — a verifier in
   * the release's PR-author set is rejected. Defaults to true. */
  ship_hub_approver_can_be_author?: boolean;
}

/**
 * Per-risk-tier approval rule values. Mirrored on the Go side (
 * `ship.ApprovalRule*` constants) and the SQL CHECK constraint in
 * migration 090. Adding a value here without updating both other
 * sources of truth will surface as a 400 from PATCH /workspaces/{id}.
 */
export type ApprovalRule = "member" | "admin" | "approver" | "two";

/** Wire shape for ship_release_signoff rows returned by GET
 *  /api/releases/{id}. Phase 7d follow-up — used by the "two"
 *  approval rule to track first/second approver signoffs. */
export interface ReleaseSignoff {
  approver_slot: "first" | "second";
  signed_by: string;
  signed_at: string;
  note: string | null;
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
