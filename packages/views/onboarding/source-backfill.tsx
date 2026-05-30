"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Briefcase,
  CalendarDays,
  Globe,
  HelpCircle,
  MoreHorizontal,
  Newspaper,
  Users,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { captureEvent } from "@multica/core/analytics";
import { useAuthStore } from "@multica/core/auth";
import {
  saveQuestionnaire,
  type QuestionnaireAnswers,
  type Source,
} from "@multica/core/onboarding";
import { mergedQuestionnairePatch } from "./source-backfill-merge";
import { Button } from "@multica/ui/components/ui/button";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { DragStrip } from "@multica/views/platform";
import {
  GoogleIcon,
  LinkedInIcon,
  OpenAIIcon,
  XIcon,
  YouTubeIcon,
} from "./components/brand-icons";
import {
  IconOptionCard,
  IconOtherOptionCard,
} from "./components/icon-option-card";
import type { QuestionOption } from "./steps/step-question";
import { useT } from "../i18n";

const EMPTY_BACKFILL: Pick<
  QuestionnaireAnswers,
  "source" | "source_other" | "source_skipped"
> = {
  source: [],
  source_other: null,
  source_skipped: false,
};

/**
 * Backfill prompt shown to already-onboarded users who never recorded
 * an acquisition source. It is rendered as a full-window takeover
 * (Web: a route; Desktop: a `WindowOverlay`), reusing the same option
 * card grid as the onboarding `StepSource` so the visual language
 * matches the original flow.
 *
 * Three exit shapes:
 *   - Submit  → POST source array, person-property mirror, terminal.
 *   - Skip    → POST `source_skipped=true`, terminal (we never ask
 *               again).
 *   - Close X / ESC → call `onClose`. The caller bumps a per-user
 *               dismiss counter; once it hits the cap the predicate
 *               `needsSourceBackfill` returns false on subsequent
 *               renders, so this view stops appearing.
 *
 * No `onBack`: this is a standalone prompt, not part of a wizard, so
 * there is nowhere to go back to. The corner X provides escape.
 */
export function SourceBackfillView({
  onComplete,
  onClose,
}: {
  /** Called after a successful Submit or Skip, with the terminal
   *  reason so the caller can decide where to navigate. */
  onComplete: (reason: "submitted" | "skipped") => void;
  /** Called when the user dismisses without committing (X / ESC). */
  onClose: () => void;
}) {
  const { t } = useT("onboarding");
  const user = useAuthStore((s) => s.user);

  // Source of truth for the picker is local state; we PATCH on Submit /
  // Skip rather than per-field, so a network blip on one tick doesn't
  // create a half-written record server-side.
  const [answers, setAnswers] = useState(EMPTY_BACKFILL);
  const [pendingOther, setPendingOther] = useState(false);
  const [busy, setBusy] = useState(false);
  const shownEmittedRef = useRef(false);
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  // Fire the funnel-open event exactly once per mount. The dismiss
  // counter lives in the caller (localStorage); we don't read it here
  // because the predicate already gated us in, but we surface it as a
  // property so PostHog can split the funnel by re-prompt round.
  useEffect(() => {
    if (shownEmittedRef.current) return;
    shownEmittedRef.current = true;
    captureEvent("source_backfill_shown");
  }, []);

  // ESC closes the prompt without committing. Matches Dialog conventions
  // and gives keyboard users a fast escape that isn't a write.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (busy) return;
      e.preventDefault();
      captureEvent("source_backfill_dismissed", { trigger: "escape" });
      onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [busy, onClose]);

  const options = useMemo<QuestionOption[]>(
    () => [
      { slug: "friends_colleagues", icon: <Users className="h-4 w-4" />, label: t(($) => $.questions.source.friends_colleagues) },
      { slug: "search", icon: <GoogleIcon className="h-[18px] w-[18px]" />, label: t(($) => $.questions.source.search) },
      { slug: "social_x", icon: <XIcon className="h-[15px] w-[15px]" />, label: t(($) => $.questions.source.social_x) },
      { slug: "social_linkedin", icon: <LinkedInIcon className="h-[18px] w-[18px]" />, label: t(($) => $.questions.source.social_linkedin) },
      { slug: "social_youtube", icon: <YouTubeIcon className="h-[18px] w-[18px]" />, label: t(($) => $.questions.source.social_youtube) },
      { slug: "social_other", icon: <Globe className="h-4 w-4" />, label: t(($) => $.questions.source.social_misc) },
      { slug: "blog_newsletter", icon: <Newspaper className="h-4 w-4" />, label: t(($) => $.questions.source.blog_newsletter) },
      { slug: "ai_assistant", icon: <OpenAIIcon className="h-[16px] w-[16px]" />, label: t(($) => $.questions.source.ai_assistant) },
      { slug: "from_work", icon: <Briefcase className="h-4 w-4" />, label: t(($) => $.questions.source.from_work) },
      { slug: "event_conference", icon: <CalendarDays className="h-4 w-4" />, label: t(($) => $.questions.source.event_conference) },
      { slug: "dont_remember", icon: <HelpCircle className="h-4 w-4" />, label: t(($) => $.questions.source.dont_remember) },
      { slug: "other", icon: <MoreHorizontal className="h-4 w-4" />, label: t(($) => $.questions.source.other), isOther: true },
    ],
    [t],
  );

  const selected = useMemo<readonly string[]>(
    () => [
      ...answers.source,
      ...(!answers.source.includes("other") && answers.source_other
        ? ["other"]
        : []),
    ],
    [answers.source, answers.source_other],
  );

  const otherOption = options.find((o) => o.isOther) ?? null;
  const otherSelected = otherOption
    ? selected.includes(otherOption.slug)
    : false;
  const otherActive = otherSelected || pendingOther;
  const otherFilled = (answers.source_other ?? "").trim().length > 0;
  const hasNonOtherSelection = selected.some(
    (slug) => slug !== otherOption?.slug,
  );
  const canSubmit =
    !busy &&
    selected.length > 0 &&
    (hasNonOtherSelection || !otherActive || otherFilled);

  const handleSelect = useCallback(
    (option: QuestionOption) => {
      if (option.isOther) {
        setPendingOther(true);
        setAnswers((a) => {
          const has = a.source.includes("other");
          return has
            ? { ...a, source: a.source.filter((s) => s !== "other"), source_other: null }
            : { ...a, source: [...a.source, "other"], source_skipped: false };
        });
        return;
      }
      setPendingOther(false);
      const slug = option.slug as Source;
      setAnswers((a) => {
        const has = a.source.includes(slug);
        return {
          ...a,
          source: has ? a.source.filter((s) => s !== slug) : [...a.source, slug],
          source_skipped: false,
        };
      });
    },
    [],
  );

  const handleOtherChange = useCallback((value: string) => {
    setAnswers((a) => ({ ...a, source_other: value }));
  }, []);

  const submit = useCallback(async () => {
    if (!canSubmit) return;
    setBusy(true);
    try {
      // `PATCH /api/me/onboarding` replaces the JSONB wholesale, so we
      // re-read the stored answers from the auth store and overlay only
      // the source slots. `useAuthStore.getState()` here, not the hook
      // — we want the freshest value at click time, and adding the user
      // object to the deps would re-create `submit` on every refreshMe.
      const stored =
        useAuthStore.getState().user?.onboarding_questionnaire ?? null;
      await saveQuestionnaire(
        mergedQuestionnairePatch(stored, {
          source: answers.source,
          source_other: answers.source_other,
          source_skipped: false,
        }),
      );
      captureEvent("source_backfill_submitted", {
        source: answers.source,
        ...(answers.source_other ? { source_other: answers.source_other } : {}),
      });
      onComplete("submitted");
    } catch (err) {
      setBusy(false);
      toast.error(err instanceof Error ? err.message : "Failed to save");
    }
  }, [canSubmit, answers.source, answers.source_other, onComplete]);

  const skip = useCallback(async () => {
    if (busy) return;
    setBusy(true);
    try {
      const stored =
        useAuthStore.getState().user?.onboarding_questionnaire ?? null;
      await saveQuestionnaire(
        mergedQuestionnairePatch(stored, {
          source: [],
          source_other: null,
          source_skipped: true,
        }),
      );
      captureEvent("source_backfill_skipped");
      onComplete("skipped");
    } catch (err) {
      setBusy(false);
      toast.error(err instanceof Error ? err.message : "Failed to save");
    }
  }, [busy, onComplete]);

  const close = useCallback(() => {
    if (busy) return;
    captureEvent("source_backfill_dismissed", { trigger: "close" });
    onClose();
  }, [busy, onClose]);

  // Guard: the predicate gates entry, so user should always be defined
  // here. We render nothing if not, rather than throwing, so a transient
  // logout while this view is mounted doesn't crash the renderer.
  if (!user) return null;

  return (
    <div className="animate-onboarding-enter flex h-full min-h-0 flex-col bg-background">
      <DragStrip />
      <header className="flex shrink-0 items-center justify-between gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
        <span aria-hidden className="w-0" />
        <button
          type="button"
          onClick={close}
          aria-label={t(($) => $.common.close)}
          disabled={busy}
          className="flex h-8 w-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
        >
          <X className="h-4 w-4" />
        </button>
      </header>

      <main
        ref={mainRef}
        style={fadeStyle}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        <div className="mx-auto w-full max-w-[920px] px-6 py-10 sm:px-10 md:px-14 lg:py-14">
          <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
            {t(($) => $.source_backfill.eyebrow)}
          </div>
          <h1 className="text-balance font-serif text-[34px] font-medium leading-[1.15] tracking-tight text-foreground">
            {t(($) => $.questions.source.question)}
          </h1>
          <p className="mt-3 max-w-[640px] text-sm text-muted-foreground">
            {t(($) => $.source_backfill.lede)}
          </p>

          <fieldset
            role="group"
            aria-label={t(($) => $.questions.source.question)}
            className="m-0 mt-10 grid grid-cols-1 gap-3 p-0 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
          >
            {options.map((option) =>
              option.isOther ? (
                <IconOtherOptionCard
                  key={option.slug}
                  icon={option.icon}
                  label={option.label}
                  selected={otherActive}
                  onSelect={() => handleSelect(option)}
                  otherValue={answers.source_other ?? ""}
                  onOtherChange={handleOtherChange}
                  onConfirm={submit}
                  placeholder={t(($) => $.questions.source.other_placeholder)}
                  mode="checkbox"
                />
              ) : (
                <IconOptionCard
                  key={option.slug}
                  icon={option.icon}
                  label={option.label}
                  selected={selected.includes(option.slug)}
                  onSelect={() => handleSelect(option)}
                  mode="checkbox"
                />
              ),
            )}
          </fieldset>

          <div className="mt-8 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
            <span
              aria-live="polite"
              className="mr-auto text-xs text-muted-foreground"
            >
              {canSubmit
                ? t(($) => $.source_backfill.hint_ready)
                : t(($) => $.step_question.hint_pick)}
            </span>
            <div className="flex items-center gap-2">
              <Button size="lg" variant="secondary" disabled={busy} onClick={skip}>
                {t(($) => $.common.skip)}
              </Button>
              <Button size="lg" disabled={!canSubmit} onClick={submit}>
                {t(($) => $.source_backfill.submit)}
              </Button>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

SourceBackfillView.displayName = "SourceBackfillView";
