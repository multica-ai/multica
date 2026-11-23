import { api } from "../api";
import { useAuthStore } from "../auth";
import { setPersonProperties } from "../analytics";
import type { User } from "../types";
import type { OnboardingCompletionPath, QuestionnaireAnswers } from "./types";

/**
 * Persist questionnaire answers and sync the refreshed user into the auth
 * store. Source of truth is `user.onboarding_questionnaire` (JSONB on the
 * server). Onboarding itself no longer asks anything; the source backfill
 * prompt is the remaining writer.
 */
export async function saveQuestionnaire(
  answers: Partial<QuestionnaireAnswers>,
): Promise<void> {
  const user = await api.patchOnboarding({ questionnaire: answers });
  useAuthStore.getState().setUser(user);
  // Mirror the three cohort signals into person properties so every
  // PostHog event on this user can be broken down by source / role /
  // use_case without re-joining the DB. `source` is single-select but
  // shipped as a one-element array for v2 back-compat with the JSONB
  // column; `use_case` is multi-select. PostHog accepts array property
  // values, and breakdowns split each element into its own group — so
  // single-element source still slices cleanly.
  const sourceList = answers.source ?? [];
  const useCaseList = answers.use_case ?? [];
  if (sourceList.length > 0 || answers.role || useCaseList.length > 0) {
    setPersonProperties({
      ...(sourceList.length > 0 ? { source: sourceList } : {}),
      ...(answers.role ? { role: answers.role } : {}),
      ...(useCaseList.length > 0 ? { use_case: useCaseList } : {}),
    });
  }
}

/**
 * The server counts `onboarding_started` on a user's first questionnaire
 * write, so entering onboarding writes the empty v2 questionnaire once.
 */
export async function recordOnboardingStarted(user: User): Promise<void> {
  const stored = user.onboarding_questionnaire;
  if (stored && Object.keys(stored).length > 0) return;
  await saveQuestionnaire({ version: 2 });
}

/**
 * Finalize onboarding. POST /complete marks `onboarded_at` atomically
 * (COALESCE-guarded for idempotency) and emits the `onboarding_completed`
 * analytics event exactly once. We then refresh the auth store so every
 * gate sees the updated user — most importantly the workspace layout
 * hard gate that redirects un-onboarded users back to /onboarding.
 *
 * This is the only frontend mechanism that flips `onboarded_at`.
 * Runtime-connected onboarding creates Mika and enqueues the hidden opening
 * turn before calling this function. The explicit no-runtime path may seed
 * one setup-guide issue after navigation.
 *
 * `completionPath` is the client's view of which Step-3 exit the user
 * took; the server funnel-splits `onboarding_completed` on this value.
 * Legacy callers that don't pass a path get recorded as `unknown`.
 */
export async function completeOnboarding(
  completionPath?: OnboardingCompletionPath,
  workspaceId?: string,
): Promise<void> {
  await api.markOnboardingComplete(
    completionPath || workspaceId
      ? { completion_path: completionPath, workspace_id: workspaceId }
      : undefined,
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
