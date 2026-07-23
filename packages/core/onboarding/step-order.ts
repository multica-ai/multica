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
 * Two questions are intentionally NOT steps anymore:
 *
 *   - "source" (How did you hear about Multica?) is pure attribution
 *     data with zero user-facing payoff, so it no longer taxes the
 *     critical path. It is collected post-onboarding by the workspace
 *     source-backfill prompt, and only after agents have completed
 *     work for the user — see `needs-backfill.ts`.
 *   - "role" / "use_case" merged into the single "about_you" step:
 *     they share the same consumer (the Helper "About me" context
 *     block) and one screen keeps the flow at three steps.
 *
 * Note: "teammate" (the old "Create your first agent" step) is no longer
 * part of the in-flow sequence. Helper agent creation now happens after
 * onboarding exits, in the workspace shell — see
 * `packages/views/workspace/welcome-after-onboarding.tsx`.
 */
export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "about_you",
  "workspace",
  "runtime",
] as const;
