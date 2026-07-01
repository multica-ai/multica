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
} from "lucide-react";
import { toast } from "sonner";
import { captureEvent } from "@multica/core/analytics";
import { useAuthStore } from "@multica/core/auth";
import {
  needsSourceBackfill,
  saveQuestionnaire,
  shouldShowSourceChannelReporting,
  type QuestionnaireAnswers,
  type Source,
} from "@multica/core/onboarding";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
} from "@multica/ui/components/ui/dialog";
import {
  GitHubIcon,
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
import { SourceReportingControls } from "./source-reporting-controls";
import { mergedQuestionnairePatch } from "./source-backfill-merge";
import { useSourceBackfillDismissCount } from "./source-backfill-dismiss";
import { useT } from "../i18n";

const EMPTY_BACKFILL: Pick<
  QuestionnaireAnswers,
  "source" | "source_other" | "source_skipped" | "source_domain_consent"
> = {
  source: [],
  source_other: null,
  source_skipped: false,
  source_domain_consent: true,
};

/**
 * Source-attribution backfill prompt for already-onboarded users whose
 * questionnaire never recorded a source. Rendered as a Dialog overlay
 * on top of the workspace shell — the user keeps their workspace
 * context visible behind a dimmed backdrop.
 *
 * Self-mounted: the caller drops `<SourceBackfillModal />` once inside
 * the dashboard layout. The component reads the predicate
 * `needsSourceBackfill(user, dismissCount)` and opens the dialog when
 * it flips to true. Once the dialog opens we capture the open decision
 * in a ref so subsequent re-renders that flip the predicate to false
 * (e.g. after submit, before refreshMe round-trips) don't tear the
 * dialog away mid-animation.
 *
 * Three exit shapes:
 *   - Submit         → PATCH merged questionnaire, terminal.
 *   - Skip           → PATCH `source_skipped=true`, terminal (never
 *                       ask again).
 *   - Close X / ESC  → bump per-user dismiss counter; predicate
 *                       returns false on next mount once the cap is
 *                       reached.
 *
 * State persistence is intentional:
 *   - source / source_other: server (JSONB merged with prior answers).
 *   - dismissCount: per-user localStorage, view-layer only.
 */
export function SourceBackfillModal() {
  const user = useAuthStore((s) => s.user);
  const userId = user?.id ?? null;
  const [dismissCount, bumpDismissCount] =
    useSourceBackfillDismissCount(userId);

  // Decide once per (user, dismissCount delta) whether the prompt
  // should open. After it opens we stop reconsulting the predicate so
  // a midflight refreshMe (which sets source) doesn't unmount the
  // dialog while the submit animation is still running. `openedRef`
  // is reset when the user identity changes.
  //
  // Strict-mode caveat: this effect runs twice in dev. The ref MUST
  // only be stamped when the dialog actually opens — not when the
  // effect schedules the timer — or the second strict-mode pass sees
  // `openedForUserRef.current === user.id` and bails without setting
  // up a new timer to replace the one strict-mode's cleanup just
  // cleared, leaving the dialog forever closed.
  const [open, setOpen] = useState(false);
  const openedForUserRef = useRef<string | null>(null);
  useEffect(() => {
    if (!user) {
      openedForUserRef.current = null;
      setOpen(false);
      return;
    }
    if (openedForUserRef.current === user.id) return;
    if (!needsSourceBackfill(user, dismissCount)) return;
    // Soft entrance: let the user see the workspace for a beat before
    // the modal floats in, so it doesn't feel like a hard block. Common
    // delight pattern — ~700ms is short enough that nobody starts an
    // interaction first but long enough that the workspace renders.
    // Honour `prefers-reduced-motion: reduce`: those users have opted
    // out of incidental animations, so open immediately.
    const reducedMotion =
      typeof window !== "undefined" &&
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
    if (reducedMotion) {
      openedForUserRef.current = user.id;
      setOpen(true);
      return;
    }
    const uid = user.id;
    const timer = window.setTimeout(() => {
      openedForUserRef.current = uid;
      setOpen(true);
    }, 700);
    return () => window.clearTimeout(timer);
  }, [user, dismissCount]);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next || !open) return;
        // Base UI fires onOpenChange(false) for X, ESC, and outside
        // click — all three count as "dismiss without committing".
        captureEvent("source_backfill_dismissed", { trigger: "close" });
        bumpDismissCount();
        setOpen(false);
      }}
    >
      <SourceBackfillDialogBody
        open={open}
        onComplete={() => setOpen(false)}
      />
    </Dialog>
  );
}

SourceBackfillModal.displayName = "SourceBackfillModal";

/**
 * Inner panel split out so its expensive setup (option list, effects)
 * only runs while the dialog is actually open. Closing the dialog
 * unmounts this subtree and clears the picker state — the next open
 * starts fresh.
 */
function SourceBackfillDialogBody({
  open,
  onComplete,
}: {
  open: boolean;
  onComplete: () => void;
}) {
  const { t } = useT("onboarding");
  const showSourceReporting = shouldShowSourceChannelReporting();

  const [answers, setAnswers] = useState(EMPTY_BACKFILL);
  const [busy, setBusy] = useState(false);
  const shownEmittedRef = useRef(false);

  // Fire the funnel-open event exactly once per open transition.
  // `open` flips back when the parent closes us, and a fresh subsequent
  // open mounts a brand-new body, so the ref starts fresh too.
  useEffect(() => {
    if (!open) return;
    if (shownEmittedRef.current) return;
    shownEmittedRef.current = true;
    captureEvent("source_backfill_shown");
  }, [open]);

  const options = useMemo<QuestionOption[]>(
    () => [
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
    ],
    [t],
  );

  // Single-select: at most one slug in `source` at any time. The server
  // schema keeps the array (back-compat with the v2 multi-select shape
  // and with existing rows), but the modal UI commits exactly one pick
  // — primary-source attribution is the documented industry default for
  // HDYHAU prompts (Fairing, Recast, HockeyStack) and gives the team
  // clean channel weights without splitting users across N buckets.
  const pickedSlug: string | null = answers.source[0] ?? null;
  const domainConsent = answers.source_domain_consent !== false;
  const otherOption = options.find((o) => o.isOther) ?? null;
  const otherSelected = pickedSlug === otherOption?.slug;
  const otherFilled = (answers.source_other ?? "").trim().length > 0;
  const canSubmit =
    !busy &&
    pickedSlug !== null &&
    // If the user picked Other, gate Submit on having typed something —
    // an empty Other selection isn't useful attribution data.
    (!otherSelected || otherFilled);

  const handleSelect = useCallback(
    (option: QuestionOption) => {
      if (option.isOther) {
        const slug = option.slug as Source;
        setAnswers((a) => ({
          ...a,
          source: [slug],
          // Picking Other doesn't carry text from a prior Other pick
          // forward: the text input auto-focuses fresh so the user can
          // type immediately. A previous text value would be misleading.
          source_other: a.source[0] === "other" ? a.source_other : null,
          source_skipped: false,
          source_domain_consent: a.source_domain_consent !== false,
        }));
        return;
      }
      const slug = option.slug as Source;
      setAnswers((a) => ({
        ...a,
        source: [slug],
        source_other: null,
        source_skipped: false,
        source_domain_consent: a.source_domain_consent !== false,
      }));
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
      // PATCH /api/me/onboarding replaces the JSONB wholesale, so we
      // re-read the stored answers from the auth store and overlay
      // only the source slots — preserving role / use_case / version
      // for the historical users this prompt targets.
      const stored =
        useAuthStore.getState().user?.onboarding_questionnaire ?? null;
      await saveQuestionnaire(
        mergedQuestionnairePatch(stored, {
          source: answers.source,
          source_other: answers.source_other,
          source_skipped: false,
          source_domain_consent: domainConsent,
        }),
      );
      captureEvent("source_backfill_submitted", {
        source: answers.source,
        ...(answers.source_other ? { source_other: answers.source_other } : {}),
      });
      onComplete();
    } catch (err) {
      setBusy(false);
      toast.error(err instanceof Error ? err.message : "Failed to save");
    }
  }, [canSubmit, answers.source, answers.source_other, domainConsent, onComplete]);

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
          source_domain_consent: domainConsent,
        }),
      );
      captureEvent("source_backfill_skipped");
      onComplete();
    } catch (err) {
      setBusy(false);
      toast.error(err instanceof Error ? err.message : "Failed to save");
    }
  }, [busy, domainConsent, onComplete]);

  return (
    <DialogContent className="flex max-h-[calc(100vh-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
      <div className="shrink-0 px-6 pt-6 pb-2">
        <div className="text-[11px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t(($) => $.source_backfill.eyebrow)}
        </div>
        <h2 className="mt-1 text-balance font-serif text-2xl font-medium leading-tight tracking-tight text-foreground">
          {t(($) => $.questions.source.question)}
        </h2>
        <p className="mt-2 text-sm text-muted-foreground">
          {t(($) => $.source_backfill.lede)}
        </p>
      </div>

      <div className="min-h-0 overflow-y-auto px-6 pt-4 pb-4">
        <fieldset
          role="radiogroup"
          aria-label={t(($) => $.questions.source.question)}
          className="m-0 grid grid-cols-1 gap-2 p-0 sm:grid-cols-2"
        >
          {options.map((option) =>
            option.isOther ? (
              <IconOtherOptionCard
                key={option.slug}
                icon={option.icon}
                label={option.label}
                selected={otherSelected}
                onSelect={() => handleSelect(option)}
                otherValue={answers.source_other ?? ""}
                onOtherChange={handleOtherChange}
                onConfirm={submit}
                placeholder={t(($) => $.questions.source.other_placeholder)}
                mode="radio"
              />
            ) : (
              <IconOptionCard
                key={option.slug}
                icon={option.icon}
                label={option.label}
                selected={pickedSlug === option.slug}
                onSelect={() => handleSelect(option)}
                mode="radio"
              />
            ),
          )}
        </fieldset>

        {showSourceReporting && pickedSlug !== null ? (
          <div className="pt-4">
            <SourceReportingControls
              domainConsent={domainConsent}
              onDomainConsentChange={(enabled) =>
                setAnswers((a) => ({
                  ...a,
                  source_domain_consent: enabled,
                }))
              }
            />
          </div>
        ) : null}
      </div>

      <div className="flex shrink-0 flex-wrap items-center justify-end gap-x-4 gap-y-2 border-t bg-muted/40 px-6 py-3">
        <span
          aria-live="polite"
          className="mr-auto text-xs text-muted-foreground"
        >
          {canSubmit
            ? t(($) => $.source_backfill.hint_ready)
            : t(($) => $.step_question.hint_pick)}
        </span>
        <div className="flex items-center gap-2">
          <Button variant="secondary" disabled={busy} onClick={skip}>
            {t(($) => $.common.skip)}
          </Button>
          <Button disabled={!canSubmit} onClick={submit}>
            {t(($) => $.source_backfill.submit)}
          </Button>
        </div>
      </div>
    </DialogContent>
  );
}
