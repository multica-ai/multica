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
  type QuestionnaireAnswers,
  type Source,
} from "@multica/core/onboarding";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
} from "@multica/ui/components/ui/dialog";
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
import { mergedQuestionnairePatch } from "./source-backfill-merge";
import { useSourceBackfillDismissCount } from "./source-backfill-dismiss";
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
    openedForUserRef.current = user.id;
    setOpen(true);
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

  const [answers, setAnswers] = useState(EMPTY_BACKFILL);
  const [pendingOther, setPendingOther] = useState(false);
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
      onComplete();
    } catch (err) {
      setBusy(false);
      toast.error(err instanceof Error ? err.message : "Failed to save");
    }
  }, [busy, onComplete]);

  return (
    <DialogContent className="sm:max-w-2xl p-0 gap-0 overflow-hidden">
      <div className="px-6 pt-6 pb-2">
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

      <fieldset
        role="group"
        aria-label={t(($) => $.questions.source.question)}
        className="m-0 grid grid-cols-1 gap-2 p-0 px-6 pt-4 sm:grid-cols-2"
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

      <div className="mt-4 flex flex-wrap items-center justify-end gap-x-4 gap-y-2 border-t bg-muted/40 px-6 py-3">
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
