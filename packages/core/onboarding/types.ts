export type OnboardingStep =
  | "welcome"
  | "source"
  | "role"
  | "use_case"
  | "workspace"
  | "runtime"
  | "teammate"
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
 * v2 questionnaire shape. `*_skipped: true` distinguishes an explicit
 * Skip click from a slot the user never reached. Both states are
 * "unknown" for recommendation purposes; the skip marker exists for
 * analytics and so future re-prompts can avoid nagging users who
 * already declined.
 */
export interface QuestionnaireAnswers {
  source: Source | null;
  source_other: string | null;
  source_skipped: boolean;
  role: Role | null;
  role_other: string | null;
  role_skipped: boolean;
  use_case: UseCase | null;
  use_case_other: string | null;
  use_case_skipped: boolean;
  version: 2;
}
