"use client";

import {
  ONBOARDING_STEP_ORDER,
  type OnboardingStep,
} from "@multica/core/onboarding";
import { cn } from "@multica/ui/lib/utils";
import { useOnboardingT } from "../i18n";

/**
 * Horizontal step indicator shown at the top of every onboarding step
 * except Welcome.
 *
 * Layout: a row of dots on the left (one per step in
 * `ONBOARDING_STEP_ORDER`) and a plaintext "Step N of M" counter on
 * the right. The dots show three states driven by the current step's
 * position in the canonical order:
 *
 *   - `done`     filled with primary color         (index < current)
 *   - `current`  filled + ring for emphasis        (index === current)
 *   - `pending`  hollow / muted                    (index > current)
 *
 * The indicator derives both its dots and text from the same source —
 * the canonical ONBOARDING_STEP_ORDER plus the caller-provided
 * `currentStep` — so adding, removing, or reordering a step only
 * requires editing the array.
 *
 * Not rendered on the Welcome screen: the caller (OnboardingFlow)
 * decides whether to include this component based on whether the
 * current render step is "welcome". See flow orchestrator for the
 * mapping from local UI step to the canonical `OnboardingStep`.
 */
export function StepHeader({ currentStep }: { currentStep: OnboardingStep }) {
  const t = useOnboardingT();
  const total = ONBOARDING_STEP_ORDER.length;
  const currentIndex = ONBOARDING_STEP_ORDER.indexOf(currentStep);
  // Defensive: unknown step → render a disabled-looking header rather
  // than throw. Happens if the caller's local step union and the store
  // enum drift during refactors.
  const safeIndex = currentIndex === -1 ? 0 : currentIndex;
  const stepLabel = t.stepHeader.stepOf(safeIndex + 1, total);

  return (
    <div
      role="progressbar"
      aria-valuemin={1}
      aria-valuemax={total}
      aria-valuenow={safeIndex + 1}
      aria-label={stepLabel}
      className="flex w-full items-center justify-between py-2"
    >
      <div className="flex items-center gap-2">
        {ONBOARDING_STEP_ORDER.map((stepId, i) => {
          const isDone = i < safeIndex;
          const isCurrent = i === safeIndex;
          return (
            <span
              key={stepId}
              aria-hidden
              className={cn(
                "h-2 w-2 rounded-full transition-colors",
                isDone && "bg-primary",
                isCurrent && "bg-primary ring-2 ring-primary/30 ring-offset-2 ring-offset-background",
                !isDone && !isCurrent && "bg-muted",
              )}
            />
          );
        })}
      </div>
      <span className="text-xs font-medium text-muted-foreground">
        {stepLabel}
      </span>
    </div>
  );
}
