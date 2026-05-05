export type OnboardingStep =
  | "welcome"
  | "questionnaire"
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
  | "full" // Reached Step 5 (first_issue) with a runtime connected
  | "runtime_skipped" // Step 3 skipped (no runtime) but still completed
  | "cloud_waitlist" // Submitted the cloud waitlist form and skipped Step 3
  | "skip_existing" // "I've done this before" from Welcome
  | "invite_accept"; // Accepted at least one invite from /invitations

export type TeamSize = "solo" | "team" | "other";

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

export interface QuestionnaireAnswers {
  team_size: TeamSize | null;
  team_size_other: string | null;
  role: Role | null;
  role_other: string | null;
  use_case: UseCase | null;
  use_case_other: string | null;
}
