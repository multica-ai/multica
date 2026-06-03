import type { OnboardingStep } from "./types";
import { configStore } from "../config";

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
 * onboarding exits, via the workspace OnboardingHelperModal — see
 * `packages/views/workspace/onboarding-helper-modal.tsx`.
 *
 * In generic mode the "runtime" step is excluded — non-IT users have no
 * concept of agent runtimes and the step would be confusing / blocking.
 * The workspace step calls `completeOnboarding("runtime_skipped", wsId)`
 * so the server-side guard is satisfied without the user seeing the step.
 */
const ALL_STEPS: readonly OnboardingStep[] = [
  "source",
  "role",
  "use_case",
  "workspace",
  "runtime",
] as const;

export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] =
  configStore.getState().genericMode
    ? ALL_STEPS.filter((s) => s !== "runtime")
    : ALL_STEPS;
