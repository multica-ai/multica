"use client";

import { type ReactNode, useRef, useState } from "react";
import { ArrowLeft } from "lucide-react";
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
 * `icon` is a React node (lucide icon or emoji span); `label` is
 * the localized string already resolved by the caller. `isOther`
 * flips this card into a free-text input row.
 */
export interface QuestionOption {
  slug: string;
  icon: ReactNode;
  label: string;
  isOther?: boolean;
}

/**
 * Generic per-question step used by Source / Role / Use case. The
 * parent threads in the question copy, the option list, and the
 * three callbacks (answer / skip / back). Layout is a centered
 * card grid with an editorial heading; bottom-left footer carries
 * Back + Skip text buttons. Click-to-advance — no Continue button.
 *
 * The "Other" path is the only exception to click-to-advance: the
 * row expands a text input and advance is gated on either Enter or
 * the Continue helper.
 */
export function StepQuestion({
  step,
  number,
  eyebrow,
  question,
  options,
  selectedSlug,
  otherValue,
  onOtherChange,
  otherPlaceholder,
  onAnswer,
  onSkip,
  onBack,
}: {
  step: OnboardingStep;
  number: number;
  eyebrow?: string;
  question: string;
  options: readonly QuestionOption[];
  selectedSlug: string | null;
  otherValue: string;
  onOtherChange: (value: string) => void;
  otherPlaceholder: string;
  onAnswer: (slug: string) => void;
  onSkip: () => void;
  onBack?: () => void;
}) {
  const { t } = useT("onboarding");
  const [pendingOther, setPendingOther] = useState(false);
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const handleSelect = (option: QuestionOption) => {
    if (option.isOther) {
      setPendingOther(true);
      onOtherChange(otherValue);
      // Switch the persisted selection to "other" but do not
      // auto-advance — wait for the user to confirm via Enter.
      onAnswer(option.slug);
      return;
    }
    setPendingOther(false);
    onAnswer(option.slug);
  };

  const confirmOther = () => {
    if (otherValue.trim()) {
      onAnswer("other");
      setPendingOther(false);
    }
  };

  const selectedOption = options.find((o) => o.slug === selectedSlug) ?? null;
  const otherActive = selectedOption?.isOther || pendingOther;

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
        <div className="mx-auto w-full max-w-[920px] px-6 py-10 sm:px-10 md:px-14 lg:py-16">
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

          <fieldset
            role="radiogroup"
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
                  onConfirm={confirmOther}
                  placeholder={otherPlaceholder}
                />
              ) : (
                <IconOptionCard
                  key={option.slug}
                  icon={option.icon}
                  label={option.label}
                  selected={selectedSlug === option.slug && !otherActive}
                  onSelect={() => handleSelect(option)}
                />
              ),
            )}
          </fieldset>
        </div>
      </main>

      <footer className="flex shrink-0 items-center gap-6 bg-background px-6 py-4 sm:px-10 md:px-14 lg:px-16">
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            {t(($) => $.common.back)}
          </button>
        ) : null}
        <button
          type="button"
          onClick={onSkip}
          className="text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          {t(($) => $.common.skip)}
        </button>
        {otherActive && otherValue.trim() ? (
          <button
            type="button"
            onClick={confirmOther}
            className="ml-auto text-sm font-medium text-foreground transition-colors hover:opacity-80"
          >
            {t(($) => $.common.continue)} →
          </button>
        ) : null}
      </footer>
    </div>
  );
}
