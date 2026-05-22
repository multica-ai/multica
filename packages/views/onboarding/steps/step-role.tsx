"use client";

import {
  Briefcase,
  Code2,
  GraduationCap,
  Megaphone,
  MoreHorizontal,
  Palette,
  PenLine,
  Rocket,
  Search,
  Settings2,
} from "lucide-react";
import type { QuestionnaireAnswers, Role } from "@multica/core/onboarding";
import { StepQuestion, type QuestionOption } from "./step-question";
import { useT } from "../../i18n";

/**
 * Step 2 — "Which best describes you?" Primary signal for the
 * onboarding assistant.
 */
export function StepRole({
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

  // Role stays single-select — the agent template recommender treats
  // role as the primary identity signal; allowing several would force
  // the recommender to pick a tiebreaker the user never expressed.
  const selectedSlug =
    answers.role ?? (answers.role_other ? "other" : null);
  const selected: readonly string[] = selectedSlug ? [selectedSlug] : [];

  return (
    <StepQuestion
      step="role"
      number={2}
      eyebrow={t(($) => $.questions.eyebrow_about_you)}
      question={t(($) => $.questions.role.question)}
      options={options}
      selectedSlugs={selected}
      otherValue={answers.role_other ?? ""}
      onOtherChange={(v) => onChange({ role_other: v })}
      otherPlaceholder={t(($) => $.questions.role.other_placeholder)}
      onAnswer={(slug) => {
        if (slug === "other") {
          onChange({ role: "other", role_skipped: false });
        } else {
          onChange({
            role: slug as Role,
            role_other: null,
            role_skipped: false,
          });
        }
      }}
      onAdvance={onAdvance}
      onSkip={() => {
        onChange({ role: null, role_other: null, role_skipped: true });
        onSkip();
      }}
      onBack={onBack}
    />
  );
}

StepRole.displayName = "StepRole";
