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
  // use_case without re-joining the DB.
  if (answers.source || answers.role || answers.use_case) {
    setPersonProperties({
      ...(answers.source ? { source: answers.source } : {}),
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
 * Runtime-connected onboarding path. The server creates or reuses the
 * default Multica Helper agent and the single onboarding issue, then
 * marks onboarding complete.
 */
export async function bootstrapRuntimeOnboarding(
  workspaceId: string,
  runtimeId: string,
): Promise<{ workspace_id: string; agent_id: string; issue_id: string }> {
  const result = await api.bootstrapOnboardingRuntime({
    workspace_id: workspaceId,
    runtime_id: runtimeId,
  });
  await useAuthStore.getState().refreshMe();
  return result;
}

/**
 * Runtime-skipped onboarding path. The server creates or reuses one
 * install-runtime onboarding issue and marks onboarding complete.
 */
export async function bootstrapNoRuntimeOnboarding(
  workspaceId: string,
): Promise<{ workspace_id: string; issue_id: string }> {
  const result = await api.bootstrapOnboardingNoRuntime({
    workspace_id: workspaceId,
  });
  await useAuthStore.getState().refreshMe();
  return result;
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
