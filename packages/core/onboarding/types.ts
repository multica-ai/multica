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
 * Questionnaire shape. `use_case` allows multiple values (users hire
 * Multica for several jobs at once); `source` and `role` are single-
 * select — for `source` we capture the primary acquisition channel
 * for clean self-reported-attribution math (the array shape is
 * preserved for back-compat with v2 multi-select rows; the client
 * now always commits a one-element array), and `role` stays single
 * because downstream personalization (the Helper "About me" context)
 * wants a primary identity.
 *
 * `role` / `use_case` are collected in-flow on the About-you step;
 * `source` is no longer asked during onboarding — it is collected
 * after the user has seen agents complete work, via the workspace
 * source-backfill prompt (see `needs-backfill.ts`). The slots stay in
 * this one shape because they share the same JSONB column and PATCH
 * endpoint.
 *
 * `*_skipped: true` distinguishes an explicit Skip / decline from a
 * slot the user never reached. Both states are "unknown" for
 * personalization purposes; the skip marker exists for analytics and
 * so future re-prompts can avoid nagging users who already declined.
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
