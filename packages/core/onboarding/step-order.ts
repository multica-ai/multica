import type { OnboardingStep } from "./types";

/**
 * Canonical onboarding order for progress and navigation. Welcome is an
 * unpersisted introduction; source attribution is collected later, after
 * agents have finished real work. Runtime is the final step: it creates the
 * member's agent and enters its chat.
 */
export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "workspace",
  "runtime",
] as const;
