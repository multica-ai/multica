"use client";

import {
  Brain,
  Code2,
  Compass,
  FileEdit,
  ListChecks,
  MoreHorizontal,
  Settings2,
  User,
} from "lucide-react";
import type { QuestionnaireAnswers, UseCase } from "@multica/core/onboarding";
import { StepQuestion, type QuestionOption } from "./step-question";
import { useT } from "../../i18n";

/**
 * Step 3 — "What do you want to use Multica for?" Tiebreaker for
 * the agent template recommendation when role alone is ambiguous.
 */
export function StepUseCase({
  answers,
  onChange,
  onAdvance,
  onSkip,
  onBack,
}: {
  answers: QuestionnaireAnswers;
  onChange: (patch: Partial<QuestionnaireAnswers>) => void;
  onAdvance: () => void;
  onSkip: () => void;
  onBack?: () => void;
}) {
  const { t } = useT("onboarding");

  const options: QuestionOption[] = [
    { slug: "ship_code", icon: <Code2 className="h-4 w-4" />, label: t(($) => $.questions.use_case.ship_code) },
    { slug: "manage_team", icon: <ListChecks className="h-4 w-4" />, label: t(($) => $.questions.use_case.manage_team) },
    { slug: "personal_tasks", icon: <User className="h-4 w-4" />, label: t(($) => $.questions.use_case.personal_tasks) },
    { slug: "plan_research", icon: <Brain className="h-4 w-4" />, label: t(($) => $.questions.use_case.plan_research) },
    { slug: "write_publish", icon: <FileEdit className="h-4 w-4" />, label: t(($) => $.questions.use_case.write_publish) },
    { slug: "automate_ops", icon: <Settings2 className="h-4 w-4" />, label: t(($) => $.questions.use_case.automate_ops) },
    { slug: "evaluate", icon: <Compass className="h-4 w-4" />, label: t(($) => $.questions.use_case.evaluate) },
    { slug: "other", icon: <MoreHorizontal className="h-4 w-4" />, label: t(($) => $.questions.use_case.other), isOther: true },
  ];

  return (
    <StepQuestion
      step="use_case"
      number={3}
      eyebrow={t(($) => $.questions.eyebrow_about_you)}
      question={t(($) => $.questions.use_case.question)}
      options={options}
      selectedSlug={answers.use_case ?? (answers.use_case_other ? "other" : null)}
      otherValue={answers.use_case_other ?? ""}
      onOtherChange={(v) => onChange({ use_case_other: v })}
      otherPlaceholder={t(($) => $.questions.use_case.other_placeholder)}
      onAnswer={(slug) => {
        if (slug === "other") {
          onChange({ use_case: "other", use_case_skipped: false });
        } else {
          onChange({
            use_case: slug as UseCase,
            use_case_other: null,
            use_case_skipped: false,
          });
        }
      }}
      onAdvance={onAdvance}
      onSkip={() => {
        onChange({ use_case: null, use_case_other: null, use_case_skipped: true });
        onSkip();
      }}
      onBack={onBack}
    />
  );
}

StepUseCase.displayName = "StepUseCase";
