"use client";

import type { CSSProperties, ReactNode } from "react";
import { ArrowLeft, Check } from "lucide-react";
import {
  ONBOARDING_STEP_ORDER,
  type OnboardingStep,
} from "@multica/core/onboarding";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { DotSphere } from "@multica/ui/components/ui/dot-sphere";
import {
  Stepper,
  StepperDescription,
  StepperIndicator,
  StepperItem,
  StepperNav,
  StepperSeparator,
  StepperTitle,
} from "@multica/ui/components/ui/stepper";
import { useT } from "../../i18n";

/**
 * The persistent rail: where the member is in onboarding, and how to go back.
 *
 * This replaces a horizontal row of dots that carried a "Step 2 of 3" counter
 * and nothing else. The counter told a member how much was left but never what
 * was coming, so each step arrived unannounced.
 *
 * Structurally it follows the ReUI onboarding-3 block: an inset panel with a
 * brand lockup at the top, the step list centred in the remaining height, and
 * a footer row — rather than a flat rail with everything crowded against the
 * top edge.
 *
 * Three things worth knowing:
 *
 *   - **`dark` on the panel, not inverted utilities.** `.dark` is a plain
 *     class selector in our token sheet, so scoping it here redefines the
 *     custom properties for this subtree only. `text-muted-foreground` then
 *     means the right thing on a dark surface. The first version of this
 *     hand-mixed `text-background/60` shades instead, which is how the Log
 *     out button ended up near-invisible.
 *   - **It is not a tablist.** The ported stepper defaults to `role="tablist"`
 *     with each trigger owning `aria-controls` on a panel id — correct for a
 *     stepper that renders its own panels, wrong here, where the panel is a
 *     routed step and those ids would dangle. We use the presentational slots
 *     and mark position with `aria-current="step"` instead.
 *   - **Only completed steps are clickable.** Forward navigation has to run
 *     each step's validation and submit, so jumping ahead from the rail would
 *     skip it; going back is always safe.
 */
/**
 * The rail's job — where am I, and how do I go back — at widths too narrow to
 * hold the rail itself. Rendered only below `md`, where StepSidebar is hidden.
 *
 * Segments rather than a repeat of the full step list: the point at 375px is
 * to spend as little vertical space as possible, and the current step's name
 * plus how far along it sits is what the list was communicating anyway. It
 * reuses the same `step_nav` copy, so there is one place to translate.
 */
export function StepProgressBar({
  currentStep,
  onBack,
  backDisabled,
  footer,
}: {
  currentStep: OnboardingStep;
  onBack?: () => void;
  backDisabled?: boolean;
  /** Same slot the rail's footer takes — the Log out escape hatch. It has to
   *  be repeated here rather than left to the rail: the rail is hidden at
   *  these widths, which stranded every step but Welcome with no way out. */
  footer?: ReactNode;
}) {
  const { t } = useT("onboarding");
  const currentIndex = Math.max(0, ONBOARDING_STEP_ORDER.indexOf(currentStep));
  const key = ONBOARDING_STEP_ORDER[currentIndex] as Exclude<
    OnboardingStep,
    "welcome"
  >;

  return (
    <div className="mb-6 flex items-center gap-3 md:hidden">
      {onBack ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={onBack}
          disabled={backDisabled}
          aria-label={t(($) => $.common.back)}
          className="-ml-2 shrink-0"
          style={{ WebkitAppRegion: "no-drag" } as CSSProperties}
        >
          <ArrowLeft />
        </Button>
      ) : null}
      <span
        aria-hidden
        className="flex flex-1 items-center gap-1.5"
      >
        {ONBOARDING_STEP_ORDER.map((stepId, index) => (
          <span
            key={stepId}
            className={cn(
              "h-1 flex-1 rounded-full transition-colors",
              index <= currentIndex ? "bg-foreground" : "bg-border",
            )}
          />
        ))}
      </span>
      <span className="min-w-0 truncate text-caption font-medium text-muted-foreground">
        {t(($) => $.step_nav[key].label)}
      </span>
      {footer ? <span className="shrink-0">{footer}</span> : null}
    </div>
  );
}

export function StepSidebar({
  currentStep,
  onBack,
  backDisabled,
  onStepChange,
  footer,
}: {
  currentStep: OnboardingStep;
  onBack?: () => void;
  /** Workspace step disables Back while its create request is in flight. */
  backDisabled?: boolean;
  /** Return to an already-completed step. Injected by the flow, which owns
   *  step order; absent means the rail is display-only. */
  onStepChange?: (step: OnboardingStep) => void;
  /** Injected by the flow — the Log out escape hatch. A slot rather than a
   *  direct render so this stays presentational: rendering the logout
   *  mutation inline forced a QueryClient into five step test files. */
  footer?: ReactNode;
}) {
  const { t } = useT("onboarding");
  const currentIndex = Math.max(0, ONBOARDING_STEP_ORDER.indexOf(currentStep));

  return (
    // Hidden below `md`, where it does not fit: the panel never went under
    // 15rem while the content pane kept its 3rem gutter, so a 375px browser
    // was left with ~87px of form. StepProgressBar carries the same
    // information — and the Back button that lives in this header — at those
    // widths.
    <aside className="hidden w-[15rem] shrink-0 p-2 md:block md:w-[19rem] md:p-3 lg:w-[22rem] lg:p-4">
      <div className="dark relative isolate flex h-full w-full flex-col overflow-hidden rounded-2xl bg-background px-5 pb-5 text-foreground ring-1 ring-border">
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 bg-background"
        >
          <DotSphere
            dotGap={19}
            motion="wave"
            sphereCount={5}
            sphereRadius="20%"
            dotRadiusMax={1.9}
            speed={0.4}
          />
        </div>

        <div className="relative flex min-h-0 flex-1 flex-col pt-5">
          <header className="flex min-h-9 shrink-0 items-center justify-between gap-3">
            <span className="flex min-w-0 items-center gap-2">
              <MulticaIcon className="size-5 shrink-0 text-foreground" noSpin />
              <span className="truncate text-label font-medium text-foreground">
                {t(($) => $.step_nav.wordmark)}
              </span>
            </span>
            {onBack ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={onBack}
                disabled={backDisabled}
                aria-label={t(($) => $.common.back)}
                style={{ WebkitAppRegion: "no-drag" } as CSSProperties}
              >
                <ArrowLeft />
              </Button>
            ) : null}
          </header>

          <div className="flex min-h-0 flex-1 items-center justify-center py-10">
            <Stepper
              value={currentIndex + 1}
              orientation="vertical"
              role="group"
              aria-label={t(($) => $.step_nav.label)}
              className="flex w-full flex-col items-start justify-center gap-0"
            >
              <StepperNav className="w-full">
                {ONBOARDING_STEP_ORDER.map((stepId, index) => {
                  const isDone = index < currentIndex;
                  const isCurrent = index === currentIndex;
                  const isLast = index === ONBOARDING_STEP_ORDER.length - 1;
                  const canReturn = isDone && !!onStepChange && !backDisabled;
                  const key = stepId as Exclude<OnboardingStep, "welcome">;

                  const body = (
                    <>
                      <StepperIndicator
                        className={cn(
                          "mt-0.5 size-4 shrink-0 border-0 bg-transparent ring-1 transition-colors",
                          isDone
                            ? "bg-foreground text-background ring-foreground"
                            : isCurrent
                              ? "text-transparent ring-muted-foreground"
                              : "text-transparent ring-border",
                        )}
                      >
                        {isDone ? (
                          <Check aria-hidden className="size-3" />
                        ) : isCurrent ? (
                          <span
                            aria-hidden
                            className="block size-1.5 rounded-full bg-foreground"
                          />
                        ) : (
                          <span className="sr-only">{index + 1}</span>
                        )}
                      </StepperIndicator>
                      <div className="min-w-0 flex-1 text-left">
                        <StepperTitle
                          className={cn(
                            "transition-colors",
                            isCurrent || isDone
                              ? "text-foreground"
                              : "text-muted-foreground",
                          )}
                        >
                          {t(($) => $.step_nav[key].label)}
                        </StepperTitle>
                        <StepperDescription className="mt-0.5 max-w-none text-muted-foreground">
                          {t(($) => $.step_nav[key].description)}
                        </StepperDescription>
                      </div>
                    </>
                  );

                  return (
                    <StepperItem
                      key={stepId}
                      step={index + 1}
                      completed={isDone}
                      className="relative w-full items-start not-last:flex-1"
                      {...(isCurrent
                        ? { "aria-current": "step" as const }
                        : {})}
                    >
                      {canReturn ? (
                        <button
                          type="button"
                          onClick={() => onStepChange(stepId)}
                          className="flex w-full items-start gap-3 rounded-md pb-6 text-left transition-opacity hover:opacity-80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          {body}
                        </button>
                      ) : (
                        <div
                          className={cn(
                            "flex w-full items-start gap-3 text-left",
                            !isLast && "pb-6",
                          )}
                        >
                          {body}
                        </div>
                      )}

                      {/* One continuous hairline behind the row rather than a
                          stub between rows, so the rail reads as a single
                          track. -order-1 puts it under the content. */}
                      {!isLast ? (
                        <StepperSeparator
                          className={cn(
                            "absolute left-2 top-6 -order-1 m-0 w-px -translate-x-1/2",
                            // StepperSeparator hardcodes a 3rem height for
                            // vertical navs, which overshot the row and drew
                            // the line straight through the next indicator.
                            // Same variant, so tailwind-merge drops theirs
                            // rather than us needing !important.
                            "group-data-[orientation=vertical]/stepper-nav:h-[calc(100%-1.75rem)]",
                            isDone ? "bg-muted-foreground" : "bg-border",
                          )}
                        />
                      ) : null}
                    </StepperItem>
                  );
                })}
              </StepperNav>
            </Stepper>
          </div>

          {footer ? (
            <footer className="flex min-h-8 shrink-0 items-end justify-between gap-4">
              {footer}
            </footer>
          ) : null}
        </div>
      </div>
    </aside>
  );
}
