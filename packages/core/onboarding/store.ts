import { api } from "../api";
import { useAuthStore } from "../auth";
import { setPersonProperties } from "../analytics";
import type { OnboardingCompletionPath, QuestionnaireAnswers } from "./types";

/**
 * Persist questionnaire answers (one or more slots at a time — each
 * onboarding step PATCHes its own slot) and sync the refreshed user
 * into the auth store. Source of truth is
 * `user.onboarding_questionnaire` (JSONB on the server). No
 * client-side cache here.
 *
 * Resume-by-step is intentionally not persisted: every onboarding
 * entry starts at Welcome. Answered slots are pre-filled on
 * re-entry; skipped slots are treated as fresh (the user can answer
 * this time).
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
 * Finalize onboarding. POST /complete marks `onboarded_at` atomically
 * (COALESCE-guarded for idempotency) and emits the `onboarding_completed`
 * analytics event exactly once. We then refresh the auth store so every
 * gate sees the updated user — most importantly the workspace layout
 * hard gate that redirects un-onboarded users back to /onboarding.
 *
 * v3 contract: this is the ONLY mechanism that flips `onboarded_at`
 * from the frontend. All Helper-agent / starter-issue creation is now
 * done by the welcome hook in the workspace shell using generic
 * `createAgent` / `createIssue` calls, AFTER this call has returned
 * and the user has been navigated into the workspace.
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
