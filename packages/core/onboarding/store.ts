import { api } from "../api";
import { useAuthStore } from "../auth";
import { setPersonProperties } from "../analytics";
import type { OnboardingCompletionPath, QuestionnaireAnswers } from "./types";

/**
 * Persist Q1/Q2/Q3 answers and sync the refreshed user into the auth
 * store. Source of truth is `user.onboarding_questionnaire` (JSONB on
 * the server). No client-side cache here.
 *
 * Resume-by-step is intentionally not persisted: every onboarding
 * entry starts at Welcome. The questionnaire is the only piece of
 * progress that survives a re-entry — it pre-fills Step 1 so the
 * user doesn't re-answer.
 */
export async function saveQuestionnaire(
  answers: Partial<QuestionnaireAnswers>,
): Promise<void> {
  const user = await api.patchOnboarding({ questionnaire: answers });
  useAuthStore.getState().setUser(user);
  // Mirror the three cohort signals into person properties so every
  // PostHog event on this user can be broken down by role / use_case /
  // team_size without re-joining the DB. Matches the $set block the
  // server writes alongside `onboarding_questionnaire_submitted`.
  if (answers.team_size || answers.role || answers.use_case) {
    setPersonProperties({
      ...(answers.team_size ? { team_size: answers.team_size } : {}),
      ...(answers.role ? { role: answers.role } : {}),
      ...(answers.use_case ? { use_case: answers.use_case } : {}),
    });
  }
}

/**
 * Finalize onboarding. POST /complete marks `onboarded_at` atomically
 * (COALESCE-guarded for idempotency). We then refresh the auth store
 * so every gate sees the updated user.
 *
 * `completionPath` is the client's view of which Step-3 exit the user
 * took; the server funnel-splits `onboarding_completed` on this value.
 * Legacy callers that don't pass a path get recorded as `unknown`.
 */
export async function completeOnboarding(
  completionPath?: OnboardingCompletionPath,
): Promise<void> {
  await api.markOnboardingComplete(
    completionPath ? { completion_path: completionPath } : undefined,
  );
  await useAuthStore.getState().refreshMe();
}

/**
 * Records interest in cloud runtimes. Pure side effect — does NOT
 * complete onboarding; the user still has to pick a real Step 3
 * path (CLI with a detected runtime) or Skip to move on.
 *
 * Returned user object is not synced into the auth store because no
 * user-visible field (`onboarded_at`, anything in `UserResponse`)
 * actually changes here.
 */
export async function joinCloudWaitlist(
  email: string,
  reason: string,
): Promise<void> {
  await api.joinCloudWaitlist({ email, reason });
}
