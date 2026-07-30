"use client";

import { ArrowLeft } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { StepHeader } from "./step-header";
import type { OnboardingStep } from "@multica/core/onboarding";

/**
 * Shared geometry for the onboarding steps, so four screens the member sees
 * back-to-back cannot drift apart again. They already had: the headers were
 * flush to the window edge while the content columns were centred, which was
 * invisible while a 480px rail absorbed the difference and became a 267px
 * misalignment the moment the rail came out.
 *
 * Three pieces, and the split between them is the point:
 *
 *   - STEP_GUTTER is the only place horizontal padding lives. Keeping it off
 *     the measured element is what stops the reading width from jumping at a
 *     breakpoint — with padding inside a max-w box, `md` and `lg` resolve to
 *     different content widths for the same box.
 *   - STEP_FRAME is the persistent chrome measure. Back and the step
 *     indicator sit on it, identically, on every step, so the one element
 *     that survives every transition never moves.
 *   - STEP_COLUMN is the reading measure for prose and forms, centred. A step
 *     may use the frame instead when its content needs the width (the
 *     questionnaire's option grid, the runtime list) and cap its own prose
 *     with STEP_MEASURE; both are centred, so content stays on the frame's
 *     centreline either way.
 */
export const STEP_GUTTER = "px-6 sm:px-10 md:px-14 lg:px-16";
export const STEP_FRAME = "mx-auto w-full max-w-[920px]";
export const STEP_COLUMN = "mx-auto w-full max-w-[620px]";

/**
 * Reading measure applied INSIDE the frame, left-aligned rather than centred.
 * Use it for prose and form fields on a step that sits on STEP_FRAME: the
 * page margins then match every other step, while a workspace name still gets
 * an input someone would want to type into instead of an 800px one.
 */
export const STEP_MEASURE = "max-w-[620px]";

/** Vertical rhythm shared by every step's scrolling region. */
export const STEP_BLOCK_PADDING = "py-10";

/**
 * Header region: Back on the left, step indicator filling the rest, both
 * bound to STEP_FRAME. Rendered by every step so the padding, the measure and
 * the bar height are decided once.
 */
export function StepShellHeader({
  currentStep,
  onBack,
  backDisabled,
}: {
  currentStep: OnboardingStep;
  onBack?: () => void;
  /** Workspace step disables Back while its create request is in flight. */
  backDisabled?: boolean;
}) {
  const { t } = useT("onboarding");
  return (
    <header className={cn("shrink-0 bg-background", STEP_GUTTER)}>
      <div className={cn(STEP_FRAME, "flex items-center gap-4 py-3")}>
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            disabled={backDisabled}
            className="flex items-center gap-1.5 text-body text-muted-foreground transition-colors hover:text-foreground disabled:opacity-40"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            {t(($) => $.common.back)}
          </button>
        ) : (
          <span aria-hidden className="w-0" />
        )}
        <div className="flex-1">
          <StepHeader currentStep={currentStep} />
        </div>
      </div>
    </header>
  );
}
