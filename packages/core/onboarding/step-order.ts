import type { OnboardingStep } from "./types";

/**
 * Canonical persisted onboarding order for progress and navigation.
 * Welcome is an unpersisted introduction; source attribution is collected later.
 * About-you combines personalization questions. Runtime is the final form step;
 * its connected path provisions Mika and enters chat without another progress step.
 */
export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "about_you",
  "workspace",
  "runtime",
] as const;
