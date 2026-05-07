import type { OnboardingStep } from "./types";

/**
 * Canonical order of the persisted onboarding steps.
 *
 * Single source of truth for "what step comes after what" — consumed
 * by the UI progress indicator to compute `index of current_step` and
 * `total step count`. Inserting, reordering, or removing a step only
 * requires changing this array; every call site that reads it updates
 * automatically.
 *
 * Intentionally excludes "welcome": welcome is a first-entry product
 * intro, not a persisted step. It doesn't show a progress indicator
 * for the same reason — users shouldn't think of reading the intro
 * as progress toward completing setup.
 */
export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "questionnaire",
  "workspace",
  "runtime",
  "agent",
  "first_issue",
] as const;
