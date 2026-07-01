"use client";

import { type ReactNode, useRef, useState } from "react";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import type { OnboardingStep } from "@multica/core/onboarding";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import {
  IconOptionCard,
  IconOtherOptionCard,
} from "../components/icon-option-card";
import { useT } from "../../i18n";

/**
 * One option in the card grid. `slug` is the persisted enum value;
 * `icon` is a React node (lucide icon, brand SVG, or emoji span);
 * `label` is the localized string already resolved by the caller.
 * `isOther` flips this card into a free-text input row.
 */
export interface QuestionOption {
  slug: string;
  icon: ReactNode;
  label: string;
  isOther?: boolean;
}

/**
 * Generic per-question step used by Source / Role / Use case. Back
 * lives in the page header next to the step indicator; Skip + Continue
 * sit inline directly below the options grid (with a status hint),
 * not in a sticky bottom footer — the form usually only fills the top
 * third of the viewport, and a footer pinned to the page bottom left a
 * large dead zone between the options and the action buttons.
 *
 * Supports both single-select (`multiSelect=false`, the default — Role
 * and Source) and multi-select (`multiSelect=true` — Use case). The
 * contract is always an array of selected slugs and an `onAnswer(slug)`
 * callback fired when a card is clicked; the parent decides whether to
 * replace (single) or toggle (multi) the selection in its own state.
 */
export function StepQuestion({
  step,
  number,
  eyebrow,
  question,
  options,
  selectedSlugs,
  otherValue,
  onOtherChange,
  otherPlaceholder,
  onAnswer,
  onAdvance,
  onSkip,
  onBack,
  multiSelect = false,
  notice,
  afterOptions,
}: {
  step: OnboardingStep;
  number: number;
  eyebrow?: string;
  question: string;
  notice?: ReactNode;
  options: readonly QuestionOption[];
  selectedSlugs: readonly string[];
  otherValue: string;
  onOtherChange: (value: string) => void;
  otherPlaceholder: string;
  /** Record the click in the parent — does NOT advance. Parent
   *  decides replace vs. toggle based on its own multi/single mode. */
  onAnswer: (slug: string) => void;
  /** Commit the current selection and move to the next step. */
  onAdvance: () => void;
  onSkip: () => void;
  onBack?: () => void;
  multiSelect?: boolean;
  afterOptions?: ReactNode;
}) {
  const { t } = useT("onboarding");
  const [pendingOther, setPendingOther] = useState(false);
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const handleSelect = (option: QuestionOption) => {
    if (option.isOther) {
      setPendingOther(true);
      onOtherChange(otherValue);
      onAnswer(option.slug);
      return;
    }
    setPendingOther(false);
    onAnswer(option.slug);
  };

  const otherOption = options.find((o) => o.isOther) ?? null;
  const otherSelected = otherOption
    ? selectedSlugs.includes(otherOption.slug)
    : false;
  const otherActive = otherSelected || pendingOther;
  const otherFilled = (otherValue ?? "").trim().length > 0;
  // Continue is enabled when:
  //   - at least one non-Other option is selected, OR
  //   - Other is selected AND the free-text input has content.
  // In multi-select (Use case) Other can stack with regular picks; we
  // treat the non-Other selections as sufficient to advance even if
  // Other's text is empty.
  const hasNonOtherSelection = selectedSlugs.some(
    (slug) => slug !== otherOption?.slug,
  );
  const canContinue =
    selectedSlugs.length > 0 &&
    (hasNonOtherSelection || !otherActive || otherFilled);

  const confirmAdvance = () => {
    if (canContinue) onAdvance();
  };

  // Footer label:
  //   - single select (Role / Source): name the picked option
  //     ("hint_selected").
  //   - multi-select (Use case) with 1 pick: same.
  //   - multi-select with >1 pick: show count via the existing
  //     "hint_continue" key (locale-friendly, doesn't grow a new key
  //     just for "N selected").
  const singlePicked =
    selectedSlugs.length === 1
      ? options.find((o) => o.slug === selectedSlugs[0]) ?? null
      : null;
  const singlePickedLabel = singlePicked?.label ?? null;
  const footerHint = canContinue
    ? singlePickedLabel
      ? t(($) => $.step_runtime.hint_selected, { name: singlePickedLabel })
      : t(($) => $.step_question.hint_continue)
    : t(($) => $.step_question.hint_pick);

  return (
    <div className="animate-onboarding-enter flex h-full min-h-0 flex-col bg-background">
      <DragStrip />
      <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            {t(($) => $.common.back)}
          </button>
        ) : (
          <span aria-hidden className="w-0" />
        )}
        <div className="flex-1">
          <StepHeader currentStep={step} />
        </div>
      </header>

      <main
        ref={mainRef}
        style={fadeStyle}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        <div className="mx-auto w-full max-w-[920px] px-6 py-10 sm:px-10 md:px-14 lg:py-14">
          {eyebrow ? (
            <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {eyebrow}
            </div>
          ) : null}
          <div className="mb-1 font-mono text-xs text-muted-foreground">
            {String(number).padStart(2, "0")}
          </div>
          <h1 className="text-balance font-serif text-[34px] font-medium leading-[1.15] tracking-tight text-foreground">
            {question}
          </h1>

          {notice ? (
            <p className="mt-3 max-w-[640px] text-sm leading-relaxed text-muted-foreground">
              {notice}
            </p>
          ) : null}

          <fieldset
            role={multiSelect ? "group" : "radiogroup"}
            aria-label={question}
            className="mt-10 m-0 grid grid-cols-1 gap-3 p-0 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
          >
            {options.map((option) =>
              option.isOther ? (
                <IconOtherOptionCard
                  key={option.slug}
                  icon={option.icon}
                  label={option.label}
                  selected={otherActive}
                  onSelect={() => handleSelect(option)}
                  otherValue={otherValue}
                  onOtherChange={onOtherChange}
                  onConfirm={confirmAdvance}
                  placeholder={otherPlaceholder}
                  mode={multiSelect ? "checkbox" : "radio"}
                />
              ) : (
                <IconOptionCard
                  key={option.slug}
                  icon={option.icon}
                  label={option.label}
                  selected={selectedSlugs.includes(option.slug)}
                  onSelect={() => handleSelect(option)}
                  mode={multiSelect ? "checkbox" : "radio"}
                />
              ),
            )}
          </fieldset>

          {afterOptions ? (
            <div className="mt-6">
              {afterOptions}
            </div>
          ) : null}

          <div className="mt-8 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
            <span
              aria-live="polite"
              className="mr-auto text-xs text-muted-foreground"
            >
              {footerHint}
            </span>
            <div className="flex items-center gap-2">
              <Button size="lg" variant="secondary" onClick={onSkip}>
                {t(($) => $.common.skip)}
              </Button>
              <Button size="lg" disabled={!canContinue} onClick={confirmAdvance}>
                {t(($) => $.common.continue)}
                <ArrowRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
