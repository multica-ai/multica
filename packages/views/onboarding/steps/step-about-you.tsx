"use client";

import {
  Brain,
  Briefcase,
  Code2,
  Compass,
  FileEdit,
  GraduationCap,
  ListChecks,
  Megaphone,
  MoreHorizontal,
  Palette,
  PenLine,
  Rocket,
  Search,
  Settings2,
  User,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { QuestionnaireAnswers, Role, UseCase } from "@multica/core/onboarding";
import {
  StepFooter,
  StepHeading,
} from "../components/step-shell";
import {
  IconOptionCard,
  IconOtherOptionCard,
  type QuestionOption,
} from "../components/icon-option-card";
import { useT } from "../../i18n";

/**
 * Step 1 — "About you": role (single-select) and use case
 * (multi-select) on ONE screen. They were separate steps once, but
 * they share the same eyebrow and neither deserves a full screen of
 * its own — merging them cut the onboarding progress bar from five
 * steps to three. The answers remain useful for product personalization
 * and onboarding analytics, independent of agent creation.
 *
 * Answering is optional per group:
 *   - Continue is enabled as soon as EITHER group has a committed
 *     answer (an "Other" pick only counts once its free-text is
 *     non-empty). On Continue, any group left unanswered gets its
 *     `*_skipped` marker — the user saw the question and moved on,
 *     which is exactly what the skip marker means downstream
 *     (analytics + never re-prompting).
 *   - Skip declines both groups at once.
 *
 * Every selection change PATCHes through `onChange` immediately (the
 * flow's `applyAnswers` fire-and-forgets persistence), so re-entry
 * pre-fills both groups.
 */
export function StepAboutYou({
  answers,
  onChange,
  onAdvance,
  onSkip,
}: {
  answers: QuestionnaireAnswers;
  onChange: (patch: Partial<QuestionnaireAnswers>) => void;
  onAdvance: () => void;
  onSkip: () => void;
}) {
  const { t } = useT("onboarding");

  const roleOptions: QuestionOption[] = [
    { slug: "engineer", icon: <Code2 className="h-4 w-4" />, label: t(($) => $.questions.role.engineer) },
    { slug: "product", icon: <Briefcase className="h-4 w-4" />, label: t(($) => $.questions.role.product) },
    { slug: "designer", icon: <Palette className="h-4 w-4" />, label: t(($) => $.questions.role.designer) },
    { slug: "founder", icon: <Rocket className="h-4 w-4" />, label: t(($) => $.questions.role.founder) },
    { slug: "marketing", icon: <Megaphone className="h-4 w-4" />, label: t(($) => $.questions.role.marketing) },
    { slug: "writer", icon: <PenLine className="h-4 w-4" />, label: t(($) => $.questions.role.writer) },
    { slug: "research", icon: <Search className="h-4 w-4" />, label: t(($) => $.questions.role.research) },
    { slug: "ops", icon: <Settings2 className="h-4 w-4" />, label: t(($) => $.questions.role.ops) },
    { slug: "student", icon: <GraduationCap className="h-4 w-4" />, label: t(($) => $.questions.role.student) },
    { slug: "other", icon: <MoreHorizontal className="h-4 w-4" />, label: t(($) => $.questions.role.other), isOther: true },
  ];

  const useCaseOptions: QuestionOption[] = [
    { slug: "ship_code", icon: <Code2 className="h-4 w-4" />, label: t(($) => $.questions.use_case.ship_code) },
    { slug: "manage_team", icon: <ListChecks className="h-4 w-4" />, label: t(($) => $.questions.use_case.manage_team) },
    { slug: "personal_tasks", icon: <User className="h-4 w-4" />, label: t(($) => $.questions.use_case.personal_tasks) },
    { slug: "plan_research", icon: <Brain className="h-4 w-4" />, label: t(($) => $.questions.use_case.plan_research) },
    { slug: "write_publish", icon: <FileEdit className="h-4 w-4" />, label: t(($) => $.questions.use_case.write_publish) },
    { slug: "automate_ops", icon: <Settings2 className="h-4 w-4" />, label: t(($) => $.questions.use_case.automate_ops) },
    { slug: "evaluate", icon: <Compass className="h-4 w-4" />, label: t(($) => $.questions.use_case.evaluate) },
    { slug: "other", icon: <MoreHorizontal className="h-4 w-4" />, label: t(($) => $.questions.use_case.other), isOther: true },
  ];

  // Role stays single-select — downstream personalization wants one
  // primary identity, not a blend.
  const roleSelected: readonly string[] = answers.role ? [answers.role] : [];
  const roleOtherFilled = (answers.role_other ?? "").trim().length > 0;
  const roleAnswered =
    answers.role !== null && (answers.role !== "other" || roleOtherFilled);

  const useCaseSlugs = answers.use_case ?? [];
  const useCaseOtherFilled =
    (answers.use_case_other ?? "").trim().length > 0;
  const useCaseHasNonOther = useCaseSlugs.some((s) => s !== "other");
  // Mirrors the old per-step rule: a lone "Other" pick only counts as
  // an answer once its free-text has content.
  const useCaseAnswered =
    useCaseSlugs.length > 0 && (useCaseHasNonOther || useCaseOtherFilled);

  const canContinue = roleAnswered || useCaseAnswered;

  const pickRole = (slug: string) => {
    if (slug === "other") {
      onChange({ role: "other", role_skipped: false });
      return;
    }
    onChange({ role: slug as Role, role_other: null, role_skipped: false });
  };

  const toggleUseCase = (slug: string) => {
    const current = answers.use_case ?? [];
    if (slug === "other") {
      if (current.includes("other")) {
        onChange({
          use_case: current.filter((s) => s !== "other"),
          use_case_other: null,
        });
      } else {
        onChange({ use_case: [...current, "other"], use_case_skipped: false });
      }
      return;
    }
    const typed = slug as UseCase;
    const next = current.includes(typed)
      ? current.filter((s) => s !== typed)
      : [...current, typed];
    onChange({ use_case: next, use_case_skipped: false });
  };

  const confirmAdvance = () => {
    if (!canContinue) return;
    // A group the user looked at but left unanswered is a decline —
    // stamp its skip marker so analytics see the decision and the
    // slot isn't treated as "never reached". Also clears a dangling
    // empty-text "Other" role pick.
    const patch: Partial<QuestionnaireAnswers> = {};
    if (!roleAnswered) {
      patch.role = null;
      patch.role_other = null;
      patch.role_skipped = true;
    }
    if (!useCaseAnswered) {
      patch.use_case = [];
      patch.use_case_other = null;
      patch.use_case_skipped = true;
    }
    if (Object.keys(patch).length > 0) onChange(patch);
    onAdvance();
  };

  const handleSkip = () => {
    onChange({
      role: null,
      role_other: null,
      role_skipped: true,
      use_case: [],
      use_case_other: null,
      use_case_skipped: true,
    });
    onSkip();
  };

  const footerHint = canContinue
    ? t(($) => $.step_question.hint_continue)
    : t(($) => $.step_question.hint_pick);

  return (
    <>
      <div className="flex flex-col gap-8 pt-2 sm:pt-6">
        <StepHeading title={t(($) => $.questions.about_you.question)} />

        <QuestionGroup
          question={t(($) => $.questions.role.question)}
          options={roleOptions}
          selectedSlugs={roleSelected}
          otherValue={answers.role_other ?? ""}
          onOtherChange={(v) => onChange({ role_other: v })}
          otherPlaceholder={t(($) => $.questions.role.other_placeholder)}
          onAnswer={pickRole}
          onConfirm={confirmAdvance}
        />

        <QuestionGroup
          question={t(($) => $.questions.use_case.question)}
          options={useCaseOptions}
          selectedSlugs={useCaseSlugs}
          otherValue={answers.use_case_other ?? ""}
          onOtherChange={(v) => onChange({ use_case_other: v })}
          otherPlaceholder={t(($) => $.questions.use_case.other_placeholder)}
          onAnswer={toggleUseCase}
          onConfirm={confirmAdvance}
          multiSelect
        />

      </div>

      <StepFooter hint={footerHint}>
        <Button className="w-full" disabled={!canContinue} onClick={confirmAdvance}>
          {t(($) => $.common.continue)}
        </Button>
        <Button variant="ghost" className="w-full" onClick={handleSkip}>
          {t(($) => $.common.skip)}
        </Button>
      </StepFooter>
    </>
  );
}

StepAboutYou.displayName = "StepAboutYou";

/**
 * One question group: sub-question label + a single column of option cards.
 * Selection semantics live in the parent's handlers; this stays a layout
 * shell so both groups render identically.
 */
function QuestionGroup({
  question,
  options,
  selectedSlugs,
  otherValue,
  onOtherChange,
  otherPlaceholder,
  onAnswer,
  onConfirm,
  multiSelect = false,
}: {
  question: string;
  options: readonly QuestionOption[];
  selectedSlugs: readonly string[];
  otherValue: string;
  onOtherChange: (value: string) => void;
  otherPlaceholder: string;
  onAnswer: (slug: string) => void;
  /** Enter inside the "Other" input — parent decides if it advances. */
  onConfirm: () => void;
  multiSelect?: boolean;
}) {
  const otherOption = options.find((o) => o.isOther) ?? null;
  const otherSelected = otherOption
    ? selectedSlugs.includes(otherOption.slug)
    : false;

  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-label font-medium text-foreground">{question}</h2>
      <fieldset
        role={multiSelect ? "group" : "radiogroup"}
        aria-label={question}
        className="m-0 flex flex-row flex-wrap gap-2 p-0"
      >
        {options.map((option) =>
          option.isOther ? (
            <IconOtherOptionCard
              key={option.slug}
              icon={option.icon}
              label={option.label}
              selected={otherSelected}
              onSelect={() => onAnswer(option.slug)}
              otherValue={otherValue}
              onOtherChange={onOtherChange}
              onConfirm={onConfirm}
              placeholder={otherPlaceholder}
              mode={multiSelect ? "checkbox" : "radio"}
            />
          ) : (
            <IconOptionCard
              key={option.slug}
              icon={option.icon}
              label={option.label}
              selected={selectedSlugs.includes(option.slug)}
              onSelect={() => onAnswer(option.slug)}
              mode={multiSelect ? "checkbox" : "radio"}
            />
          ),
        )}
      </fieldset>
    </section>
  );
}
