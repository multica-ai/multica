export type OnboardingStep =
  | "welcome"
  | "questionnaire"
  | "source"
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
  | "full" // Reached Step 5 (first_issue) with a runtime connected
  | "runtime_skipped" // Step 3 skipped (no runtime) but still completed
  | "cloud_waitlist" // Submitted the cloud waitlist form and skipped Step 3
  | "skip_existing" // "I've done this before" from Welcome
  | "invite_accept"; // Accepted at least one invite from /invitations

export type TeamSize = "solo" | "team" | "other";

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
  | "developer"
  | "product_lead"
  | "writer"
  | "founder"
  | "other";

export type UseCase =
  | "coding"
  | "planning"
  | "writing_research"
  | "explore"
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
  // CEREBRO-PATCH(onboarding-team-size): Firtal keeps the team-size question —
  // it drives the cerebro starter-content recommendation. Upstream dropped it
  // in favour of `source`; we keep both (source captured by the backfill box).
  team_size: TeamSize | null;
  team_size_other: string | null;
  source: Source[];
  source_other: string | null;
  source_skipped?: boolean;
  role: Role | null;
  role_other: string | null;
  role_skipped?: boolean;
  use_case: UseCase[];
  use_case_other: string | null;
  use_case_skipped?: boolean;
  version: 2;
}
