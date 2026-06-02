"use client";

import {
  Briefcase,
  CalendarDays,
  Globe,
  HelpCircle,
  MoreHorizontal,
  Newspaper,
  Users,
} from "lucide-react";
import type { QuestionnaireAnswers, Source } from "@multica/core/onboarding";
import {
  GoogleIcon,
  LinkedInIcon,
  OpenAIIcon,
  XIcon,
  YouTubeIcon,
  GitHubIcon,
} from "../components/brand-icons";
import { StepQuestion, type QuestionOption } from "./step-question";
import { useT } from "../../i18n";

/**
 * Step 1 — "How did you hear about Multica?" Pure attribution, does
 * not influence the agent template recommendation.
 */
export function StepSource({
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
    { slug: "friends_colleagues", icon: <Users className="h-4 w-4" />, label: t(($) => $.questions.source.friends_colleagues) },
    { slug: "search", icon: <GoogleIcon className="h-[18px] w-[18px]" />, label: t(($) => $.questions.source.search) },
    { slug: "social_x", icon: <XIcon className="h-[15px] w-[15px]" />, label: t(($) => $.questions.source.social_x) },
    { slug: "social_linkedin", icon: <LinkedInIcon className="h-[18px] w-[18px]" />, label: t(($) => $.questions.source.social_linkedin) },
    { slug: "social_youtube", icon: <YouTubeIcon className="h-[18px] w-[18px]" />, label: t(($) => $.questions.source.social_youtube) },
    { slug: "social_github", icon: <GitHubIcon className="h-[18px] w-[18px]" />, label: t(($) => $.questions.source.social_github) },
    { slug: "social_other", icon: <Globe className="h-4 w-4" />, label: t(($) => $.questions.source.social_misc) },
    { slug: "blog_newsletter", icon: <Newspaper className="h-4 w-4" />, label: t(($) => $.questions.source.blog_newsletter) },
    { slug: "ai_assistant", icon: <OpenAIIcon className="h-[16px] w-[16px]" />, label: t(($) => $.questions.source.ai_assistant) },
    { slug: "from_work", icon: <Briefcase className="h-4 w-4" />, label: t(($) => $.questions.source.from_work) },
    { slug: "event_conference", icon: <CalendarDays className="h-4 w-4" />, label: t(($) => $.questions.source.event_conference) },
    { slug: "dont_remember", icon: <HelpCircle className="h-4 w-4" />, label: t(($) => $.questions.source.dont_remember) },
    { slug: "other", icon: <MoreHorizontal className="h-4 w-4" />, label: t(($) => $.questions.source.other), isOther: true },
  ];

  // Multi-select: source already had several "yes I heard via X" intents
  // collapsed into a single radio pick. Letting users tick multiple
  // channels keeps attribution honest without making them rank them.
  // `other` is a Source enum value so it stacks into the array alongside
  // regular picks; the free-text input lives in `source_other`.
  const selected: readonly string[] = [
    ...(answers.source ?? []),
    ...(!answers.source?.includes("other") && answers.source_other
      ? ["other"]
      : []),
  ];

  const toggle = (slug: string) => {
    const current = answers.source ?? [];
    if (slug === "other") {
      // Toggling Other: add/remove from the array; clear the text when
      // removing so a re-tick starts from an empty input.
      if (current.includes("other")) {
        onChange({
          source: current.filter((s) => s !== "other"),
          source_other: null,
        });
      } else {
        onChange({
          source: [...current, "other"],
          source_skipped: false,
        });
      }
      return;
    }
    const typed = slug as Source;
    const next = current.includes(typed)
      ? current.filter((s) => s !== typed)
      : [...current, typed];
    onChange({ source: next, source_skipped: false });
  };

  return (
    <StepQuestion
      step="source"
      number={1}
      eyebrow={t(($) => $.questions.eyebrow_about_you)}
      question={t(($) => $.questions.source.question)}
      options={options}
      selectedSlugs={selected}
      otherValue={answers.source_other ?? ""}
      onOtherChange={(v) => onChange({ source_other: v })}
      otherPlaceholder={t(($) => $.questions.source.other_placeholder)}
      onAnswer={toggle}
      onAdvance={onAdvance}
      onSkip={() => {
        onChange({ source: [], source_other: null, source_skipped: true });
        onSkip();
      }}
      onBack={onBack}
      multiSelect
    />
  );
}

StepSource.displayName = "StepSource";
