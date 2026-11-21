import type { OnboardingStep } from "./types";

/**
 * Persisted onboarding order for progress. Welcome is an unpersisted
 * introduction. Web adds `connect` in front: a browser cannot run an agent CLI,
 * so it first needs a machine with a daemon. Workspace naming only runs when
 * someone who already uses Multica creates another workspace.
 */
export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "tool",
  "first_task",
] as const;

export const WEB_ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "connect",
  "tool",
  "first_task",
] as const;
