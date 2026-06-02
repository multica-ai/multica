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
 * Questionnaire shape. `source` and `use_case` allow multiple values
 * (users hear about us through several channels and use Multica for
 * several things); `role` stays single-select since the agent template
 * recommendation wants a primary identity.
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
