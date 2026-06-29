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
import { useConfigStore } from "@multica/core/config";
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
  // Self-host onboarding source beacon (MUL-3708): a production self-host
  // anonymously reports the chosen channel to Multica, so disclose it here.
  // The flag is true only on those deployments; official cloud shows nothing.
  const showSelfHostNotice = useConfigStore((s) => s.selfHostSourceNotice);

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

  // Single-select on the primary acquisition source. The server schema
  // keeps `source` as a string array for back-compat with v2 multi-
  // select rows, but the UI only ever commits a one-element array —
  // primary-source attribution is the documented industry default for
  // HDYHAU prompts (Fairing, Recast, HockeyStack) and keeps channel
  // weights clean for analytics.
  const selected: readonly string[] = answers.source?.[0] ? [answers.source[0]] : [];

  const pick = (slug: string) => {
    const typed = slug as Source;
    onChange({
      source: [typed],
      // Switching off Other clears the free-text input so a stale
      // value can't leak into the next pick. Picking Other keeps the
      // existing text so the user can finish typing without losing
      // their input.
      source_other: typed === "other" ? answers.source_other : null,
      source_skipped: false,
    });
  };

  return (
    <StepQuestion
      step="source"
      number={1}
      eyebrow={t(($) => $.questions.eyebrow_about_you)}
      question={t(($) => $.questions.source.question)}
      notice={showSelfHostNotice ? t(($) => $.questions.source.self_host_notice) : undefined}
      options={options}
      selectedSlugs={selected}
      otherValue={answers.source_other ?? ""}
      onOtherChange={(v) => onChange({ source_other: v })}
      otherPlaceholder={t(($) => $.questions.source.other_placeholder)}
      onAnswer={pick}
      onAdvance={onAdvance}
      onSkip={() => {
        onChange({ source: [], source_other: null, source_skipped: true });
        onSkip();
      }}
      onBack={onBack}
    />
  );
}

StepSource.displayName = "StepSource";
