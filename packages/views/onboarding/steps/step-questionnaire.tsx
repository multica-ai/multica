"use client";

import { type ReactNode, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Loader2,
  PenLine,
  Sparkles,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "@multica/i18n/react";
import { openExternal, publicAppUrl } from "../../platform";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import type {
  QuestionnaireAnswers,
  Role,
  TeamSize,
  UseCase,
} from "@multica/core/onboarding";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import { OptionCard, OtherOptionCard } from "../components/option-card";

/**
 * Step 1 — three-question user profile.
 *
 * Classic app-shell layout: the left column is 3-region
 * (header / scrollable middle / footer) so the progress indicator
 * and the Continue CTA both stay visible regardless of how far the
 * user has scrolled into the questions. The right "Why we ask" panel
 * is a separate grid column that scrolls independently.
 *
 * Below lg the right panel hides and the left column fills the
 * viewport — 3-region layout still applies.
 */
export function StepQuestionnaire({
  initial,
  onSubmit,
  onBack,
}: {
  initial: QuestionnaireAnswers;
  onSubmit: (answers: QuestionnaireAnswers) => void | Promise<void>;
  onBack?: () => void;
}) {
  const [answers, setAnswers] = useState<QuestionnaireAnswers>(initial);
  const [submitting, setSubmitting] = useState(false);
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);
  const t = useT("onboarding");

  const setTeamSize = (v: TeamSize) =>
    setAnswers((a) => ({
      ...a,
      team_size: v,
      team_size_other: v === "other" ? a.team_size_other : null,
    }));
  const setRole = (v: Role) =>
    setAnswers((a) => ({
      ...a,
      role: v,
      role_other: v === "other" ? a.role_other : null,
    }));
  const setUseCase = (v: UseCase) =>
    setAnswers((a) => ({
      ...a,
      use_case: v,
      use_case_other: v === "other" ? a.use_case_other : null,
    }));

  // A question counts as "answered" when it has a concrete selection,
  // and — if that selection is "other" — its free-text field is non-empty.
  // Same rule that used to drive canContinue; we compute the per-question
  // booleans once here and derive both the count (footer indicator) and
  // the overall gate from it.
  const answeredCount = useMemo(() => {
    const q1 =
      answers.team_size !== null &&
      (answers.team_size !== "other" ||
        (answers.team_size_other ?? "").trim() !== "");
    const q2 =
      answers.role !== null &&
      (answers.role !== "other" || (answers.role_other ?? "").trim() !== "");
    const q3 =
      answers.use_case !== null &&
      (answers.use_case !== "other" ||
        (answers.use_case_other ?? "").trim() !== "");
    return (q1 ? 1 : 0) + (q2 ? 1 : 0) + (q3 ? 1 : 0);
  }, [answers]);
  const canContinue = answeredCount === 3;

  const submit = async () => {
    if (!canContinue || submitting) return;
    setSubmitting(true);
    try {
      await onSubmit(answers);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      {/* Left column — DragStrip + 3-region app shell */}
      <div className="flex min-h-0 flex-col">
        <DragStrip />
        {/* Fixed header — Back + progress indicator */}
        <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
          {onBack ? (
            <button
              type="button"
              onClick={onBack}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              {t("back")}
            </button>
          ) : (
            <span aria-hidden className="w-0" />
          )}
          <div className="flex-1">
            <StepHeader currentStep="questionnaire" />
          </div>
        </header>

        {/* Scrollable middle — the only region that scrolls vertically.
            `min-h-0` is required on a flex-1 child inside a flex column
            so it can shrink below its content height and let
            overflow-y-auto activate. `useScrollFade` applies a dynamic
            mask-image gradient so content softly fades into the header /
            footer at the edges as the user scrolls, replacing the hard
            border separator. */}
        <main
          ref={mainRef}
          style={fadeStyle}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          <div className="mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
            <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {t("sq_before_we_start")}
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              {t("sq_headline")}
            </h1>

            <div className="mt-10 flex flex-col gap-7">
              <QuestionBlock
                num={1}
                question={t("sq_q1")}
                ariaLabel={t("sq_q1")}
              >
                <OptionCard
                  selected={answers.team_size === "solo"}
                  onSelect={() => setTeamSize("solo")}
                  label={t("sq_q1_solo")}
                />
                <OptionCard
                  selected={answers.team_size === "team"}
                  onSelect={() => setTeamSize("team")}
                  label={t("sq_q1_team")}
                />
                <OtherOptionCard
                  selected={answers.team_size === "other"}
                  onSelect={() => setTeamSize("other")}
                  otherValue={answers.team_size_other ?? ""}
                  onOtherChange={(v) =>
                    setAnswers((a) => ({ ...a, team_size_other: v }))
                  }
                  placeholder={t("q_team_placeholder")}
                />
              </QuestionBlock>

              <QuestionBlock
                num={2}
                question={t("sq_q2")}
                ariaLabel={t("sq_q2")}
              >
                <OptionCard
                  selected={answers.role === "developer"}
                  onSelect={() => setRole("developer")}
                  label={t("sq_q2_developer")}
                />
                <OptionCard
                  selected={answers.role === "product_lead"}
                  onSelect={() => setRole("product_lead")}
                  label={t("sq_q2_product_lead")}
                />
                <OptionCard
                  selected={answers.role === "writer"}
                  onSelect={() => setRole("writer")}
                  label={t("sq_q2_writer")}
                />
                <OptionCard
                  selected={answers.role === "founder"}
                  onSelect={() => setRole("founder")}
                  label={t("sq_q2_founder")}
                />
                <OtherOptionCard
                  selected={answers.role === "other"}
                  onSelect={() => setRole("other")}
                  otherValue={answers.role_other ?? ""}
                  onOtherChange={(v) =>
                    setAnswers((a) => ({ ...a, role_other: v }))
                  }
                  placeholder={t("q_role_placeholder")}
                />
              </QuestionBlock>

              <QuestionBlock
                num={3}
                question={t("sq_q3")}
                ariaLabel={t("sq_q3")}
              >
                <OptionCard
                  selected={answers.use_case === "coding"}
                  onSelect={() => setUseCase("coding")}
                  label={t("sq_q3_coding")}
                />
                <OptionCard
                  selected={answers.use_case === "planning"}
                  onSelect={() => setUseCase("planning")}
                  label={t("sq_q3_planning")}
                />
                <OptionCard
                  selected={answers.use_case === "writing_research"}
                  onSelect={() => setUseCase("writing_research")}
                  label={t("sq_q3_writing_research")}
                />
                <OptionCard
                  selected={answers.use_case === "explore"}
                  onSelect={() => setUseCase("explore")}
                  label={t("sq_q3_explore")}
                />
                <OtherOptionCard
                  selected={answers.use_case === "other"}
                  onSelect={() => setUseCase("other")}
                  otherValue={answers.use_case_other ?? ""}
                  onOtherChange={(v) =>
                    setAnswers((a) => ({ ...a, use_case_other: v }))
                  }
                  placeholder={t("q_goal_placeholder")}
                />
              </QuestionBlock>
            </div>
          </div>
        </main>

        {/* Fixed footer — progress counter + Continue */}
        <footer className="flex shrink-0 items-center justify-end gap-4 bg-background px-6 py-4 sm:px-10 md:px-14 lg:px-16">
          <span
            aria-live="polite"
            className="text-xs tabular-nums text-muted-foreground"
          >
            {t("sq_progress", { count: answeredCount })}
          </span>
          <Button
            size="lg"
            disabled={!canContinue || submitting}
            onClick={submit}
          >
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            {t("sq_continue")}
            <ArrowRight className="h-4 w-4" />
          </Button>
        </footer>
      </div>

      {/* Right — DragStrip + "Why we ask" side panel, independent scroll */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          <WhyWeAsk />
        </div>
      </aside>
    </div>
  );
}

function QuestionBlock({
  num,
  question,
  ariaLabel,
  children,
}: {
  num: number;
  question: string;
  ariaLabel: string;
  children: ReactNode;
}) {
  return (
    <fieldset role="radiogroup" aria-label={ariaLabel} className="m-0 p-0">
      <legend className="mb-3 flex items-baseline gap-3">
        <span className="font-mono text-xs text-muted-foreground">
          {String(num).padStart(2, "0")}
        </span>
        <span className="font-serif text-[22px] font-medium leading-tight tracking-tight text-foreground">
          {question}
        </span>
      </legend>
      <div className="flex flex-col gap-2">{children}</div>
    </fieldset>
  );
}

function WhyWeAsk() {
  const t = useT("onboarding");
  return (
    <div className="flex max-w-[380px] flex-col gap-8">
      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t("sq_why_title")}
        </div>
        <h2 className="font-serif text-[22px] font-medium leading-[1.25] tracking-tight text-foreground">
          {t("sq_why_heading")}
        </h2>
      </section>

      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t("sq_what_you_get")}
        </div>
        <div className="flex flex-col gap-4">
          <UnlockItem
            icon={<PenLine className="h-4 w-4" />}
            title={t("q_starter_project")}
            body={t("sq_starter_body")}
          />
          <UnlockItem
            icon={<Sparkles className="h-4 w-4" />}
            title={t("q_head_start_agents")}
            body={t("sq_head_start_body")}
          />
        </div>
      </section>

      <button
        type="button"
        onClick={() => openExternal(publicAppUrl("/docs/agents"))}
        className="self-start text-[13px] text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
      >
        {t("sq_learn_agents")}
      </button>
    </div>
  );
}

function UnlockItem({
  icon,
  title,
  body,
}: {
  icon: ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="grid grid-cols-[22px_1fr] gap-3">
      <div
        aria-hidden
        className="flex h-[20px] w-[20px] items-center justify-center text-muted-foreground"
      >
        {icon}
      </div>
      <div className="flex flex-col">
        <div className="text-[13.5px] font-medium text-foreground">{title}</div>
        <div className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
          {body}
        </div>
      </div>
    </div>
  );
}
