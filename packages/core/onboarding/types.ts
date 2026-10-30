export type OnboardingStep =
  | "welcome"
  | "about_you"
  | "workspace"
  | "runtime";

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
 * Questionnaire persistence shared by onboarding and source backfill.
 * use_case is multi-select; source and role are single-select. Source retains an
 * array shape for stored compatibility and is collected after agent work completes.
 * Explicit *_skipped flags distinguish declines from unanswered fields.
 *
 * Readers normalize older single-string source/use_case values to arrays.
 * Version 2 remains the shared analytics cohort; the JSONB storage is schema-less.
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
