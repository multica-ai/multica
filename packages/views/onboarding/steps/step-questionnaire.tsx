"use client";

import { type ReactNode, useMemo, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
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

  const canContinue = useMemo(() => {
    const allAnswered =
      answers.team_size !== null &&
      answers.role !== null &&
      answers.use_case !== null;
    if (!allAnswered) return false;
    const otherIncomplete =
      (answers.team_size === "other" &&
        (answers.team_size_other ?? "").trim() === "") ||
      (answers.role === "other" && (answers.role_other ?? "").trim() === "") ||
      (answers.use_case === "other" &&
        (answers.use_case_other ?? "").trim() === "");
    return !otherIncomplete;
  }, [answers]);

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
    <div className="grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
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
              Back
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
              A few quick questions
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              Three answers. We&apos;ll handle the rest.
            </h1>
            <p className="mt-3 text-[15.5px] leading-[1.55] text-foreground/80">
              Tell us a little about you. We&apos;ll use it to pick the right
              first agent, draft your first task, and hide every feature you
              don&apos;t need yet — so you start on work, not on setup.
            </p>

            <div className="mt-8 flex flex-col gap-7">
              <QuestionBlock
                num={1}
                question="Who will use this workspace?"
                ariaLabel="Who will use this workspace?"
              >
                <OptionCard
                  selected={answers.team_size === "solo"}
                  onSelect={() => setTeamSize("solo")}
                  label="Just me"
                />
                <OptionCard
                  selected={answers.team_size === "team"}
                  onSelect={() => setTeamSize("team")}
                  label="My team (2–10 people)"
                />
                <OtherOptionCard
                  selected={answers.team_size === "other"}
                  onSelect={() => setTeamSize("other")}
                  otherValue={answers.team_size_other ?? ""}
                  onOtherChange={(v) =>
                    setAnswers((a) => ({ ...a, team_size_other: v }))
                  }
                  placeholder="e.g. a small community I help run"
                />
              </QuestionBlock>

              <QuestionBlock
                num={2}
                question="What best describes you?"
                ariaLabel="What best describes you?"
              >
                <OptionCard
                  selected={answers.role === "developer"}
                  onSelect={() => setRole("developer")}
                  label="Software developer"
                />
                <OptionCard
                  selected={answers.role === "product_lead"}
                  onSelect={() => setRole("product_lead")}
                  label="Product or project lead"
                />
                <OptionCard
                  selected={answers.role === "writer"}
                  onSelect={() => setRole("writer")}
                  label="Writer or content creator"
                />
                <OptionCard
                  selected={answers.role === "founder"}
                  onSelect={() => setRole("founder")}
                  label="Founder / solo operator"
                />
                <OtherOptionCard
                  selected={answers.role === "other"}
                  onSelect={() => setRole("other")}
                  otherValue={answers.role_other ?? ""}
                  onOtherChange={(v) =>
                    setAnswers((a) => ({ ...a, role_other: v }))
                  }
                  placeholder="e.g. researcher, designer, ops lead"
                />
              </QuestionBlock>

              <QuestionBlock
                num={3}
                question="What do you want to do first?"
                ariaLabel="What do you want to do first?"
              >
                <OptionCard
                  selected={answers.use_case === "coding"}
                  onSelect={() => setUseCase("coding")}
                  label="Write and ship code"
                />
                <OptionCard
                  selected={answers.use_case === "planning"}
                  onSelect={() => setUseCase("planning")}
                  label="Plan and manage projects"
                />
                <OptionCard
                  selected={answers.use_case === "writing_research"}
                  onSelect={() => setUseCase("writing_research")}
                  label="Research or write"
                />
                <OptionCard
                  selected={answers.use_case === "explore"}
                  onSelect={() => setUseCase("explore")}
                  label="Just explore what's possible"
                />
                <OtherOptionCard
                  selected={answers.use_case === "other"}
                  onSelect={() => setUseCase("other")}
                  otherValue={answers.use_case_other ?? ""}
                  onOtherChange={(v) =>
                    setAnswers((a) => ({ ...a, use_case_other: v }))
                  }
                  placeholder="e.g. automate my weekly reports"
                />
              </QuestionBlock>
            </div>
          </div>
        </main>

        {/* Fixed footer — hint + Continue */}
        <footer className="flex shrink-0 items-center justify-between gap-4 bg-background px-6 py-4 sm:px-10 md:px-14 lg:px-16">
          <span className="hidden text-xs text-muted-foreground sm:block">
            Your answers shape the next screens. You can change anything later.
          </span>
          <Button
            size="lg"
            disabled={!canContinue || submitting}
            onClick={submit}
            className="ml-auto"
          >
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            Continue
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
  return (
    <div className="flex flex-col gap-6">
      <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        Why we ask
      </div>
      <p className="text-[14px] leading-[1.55] text-foreground/80">
        Three quick questions help us skip the setup gauntlet. Instead of
        handing you an empty board, we&apos;ll tailor the next screens to
        match how you work.
      </p>

      <div className="mt-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        What your answers unlock
      </div>
      <div className="flex flex-col gap-4">
        <UnlockItem
          num="01"
          title="A recommended first agent"
          body="Coder, planner, writer, or assistant — picked to match your role."
        />
        <UnlockItem
          num="02"
          title="A drafted first task"
          body="So you land on real work in 30 seconds, not a blank textarea."
        />
        <UnlockItem
          num="03"
          title="A curated Getting Started project"
          body="A short tour of features that actually matter to you."
        />
      </div>

      <div className="mt-2 rounded-lg border bg-card/60 px-4 py-3 text-xs leading-[1.55] text-muted-foreground">
        We&apos;ll never ask what tools you have installed — your machine
        tells us in the next step.
      </div>
    </div>
  );
}

function UnlockItem({
  num,
  title,
  body,
}: {
  num: string;
  title: string;
  body: string;
}) {
  return (
    <div className="grid grid-cols-[28px_1fr] gap-3">
      <div className="flex h-[22px] w-[22px] items-center justify-center rounded-full bg-background font-mono text-[10.5px] font-semibold text-muted-foreground">
        {num}
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
