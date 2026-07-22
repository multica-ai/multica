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
 *
 * Note: "teammate" (the old "Create your first agent" step) is no longer
 * part of the in-flow sequence. Helper agent creation now happens after
 * onboarding exits, in the workspace shell — see
 * `packages/views/workspace/welcome-after-onboarding.tsx`.
 */
export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "source",
  "role",
  "use_case",
  "workspace",
  "runtime",
] as const;
