import type { Issue } from "../types";

export type OnboardingStep =
  | "welcome"
  | "source"
  | "role"
  | "use_case"
  | "workspace"
  | "runtime"
  | "agent"
  | "first_issue";

/**
 * Exit path from the onboarding flow. Sent to
 * POST /api/me/onboarding/complete and mirrored on the PostHog
 * `onboarding_completed` event. Must stay in sync with the
 * `OnboardingPath*` constants in `server/internal/analytics/events.go`.
 */
export type OnboardingCompletionPath =
  | "full"
  | "runtime_skipped"
  | "cloud_waitlist"
  | "skip_existing"
  | "invite_accept";

/**
 * Placeholder tokens for POST /api/me/onboarding/no-runtime-seed. The
 * skip-path bundle's cross-references (agent-guide body → install issue,
 * follow-up comment → agent-guide issue) need each target's identifier +
 * uuid, which only exist once the server has created the rows — so the
 * client embeds these literal tokens and the server substitutes the real
 * `[IDENT](mention://issue/<uuid>)` chips mid-transaction. Must match the
 * constants in server/internal/handler/onboarding_seed.go.
 */
export const INSTALL_ISSUE_REF_TOKEN = "{{install_issue_ref}}";
export const AGENT_GUIDE_REF_TOKEN = "{{agent_guide_ref}}";

export interface SeedOnboardingNoRuntimeRequest {
  workspace_id: string;
  install_issue: { title: string; description: string };
  agent_guide_issue: { title: string; description: string };
  followup_comment: { content: string };
}

/**
 * The seeded bundle. Issues come back system-attributed
 * (creator_type "system") and assigned to the calling member.
 */
export interface SeedOnboardingNoRuntimeResult {
  workspace_id: string;
  install_issue: Issue;
  agent_guide_issue: Issue;
}

export type Source =
  | "friends_colleagues"
  | "search"
  | "social_x"
  | "social_linkedin"
  | "social_youtube"
  | "social_github"
  | "social_other"
  | "blog_newsletter"
  | "ai_assistant"
  | "from_work"
  | "event_conference"
  | "dont_remember"
  | "other";

export type Role =
  | "engineer"
  | "product"
  | "designer"
  | "founder"
  | "marketing"
  | "writer"
  | "research"
  | "ops"
  | "student"
  | "other";

export type UseCase =
  | "ship_code"
  | "manage_team"
  | "personal_tasks"
  | "plan_research"
  | "write_publish"
  | "automate_ops"
  | "evaluate"
  | "other";

/**
 * Questionnaire shape. `use_case` allows multiple values (users hire
 * Multica for several jobs at once); `source` and `role` are single-
 * select — for `source` we capture the primary acquisition channel
 * for clean self-reported-attribution math (the array shape is
 * preserved for back-compat with v2 multi-select rows; the client
 * now always commits a one-element array), and `role` stays single
 * because the agent template recommendation wants a primary identity.
 *
 * `*_skipped: true` distinguishes an explicit Skip click from a slot
 * the user never reached. Both states are "unknown" for recommendation
 * purposes; the skip marker exists for analytics and so future
 * re-prompts can avoid nagging users who already declined.
 *
 * Backward compat: prior versions of this app wrote `source` and
 * `use_case` as a single string. `mergeQuestionnaire` in
 * `onboarding-flow.tsx` upgrades those rows to single-element arrays
 * on read; the server's `questionnaireAnswers.UnmarshalJSON` does the
 * same. `version` stays at 2 — the JSONB column is schema-less so a
 * mechanical bump would only show up in analytics, not in storage,
 * and we keep one funnel cohort.
 */
export interface QuestionnaireAnswers {
  source: Source[];
  source_other: string | null;
  source_skipped: boolean;
  role: Role | null;
  role_other: string | null;
  role_skipped: boolean;
  use_case: UseCase[];
  use_case_other: string | null;
  use_case_skipped: boolean;
  version: 2;
}
