"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Bot, Hourglass, Lock, Sparkles, Users, X } from "@multica/ui/icons";
import { autopilotQuotaUsageOptions } from "@multica/core/autopilots";
import {
  trialNoticeKey,
  useTrialNoticeStore,
  useWorkspaceTrial,
  workspaceFeaturesOptions,
  type TrialNotice,
  type WorkspaceTrialView,
} from "@multica/core/billing";
import { useChatStore } from "@multica/core/chat";
import { useFeatureEnabled } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { useSessionPromptStore } from "@multica/core/session-prompt";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { isFloatingChatRouteSuppressed } from "../chat/floating-chat-visibility";
import { useLocale, useT } from "../i18n";
import { useNavigation } from "../navigation";

type ActiveTrial = Extract<WorkspaceTrialView, { kind: "active" }>;
type EndedTrial = Extract<WorkspaceTrialView, { kind: "ended" }>;

function useNoticeSeen(wsId: string, endsAt: number, notice: TrialNotice) {
  const key = trialNoticeKey(wsId, endsAt, notice);
  const seen = useTrialNoticeStore((s) => s.seen.includes(key));
  const markSeenKey = useTrialNoticeStore((s) => s.markSeen);
  const markSeen = useCallback(() => markSeenKey(key), [key, markSeenKey]);
  return [seen, markSeen] as const;
}

function formatDate(value: number, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}

/**
 * The trial's one-time notices (MUL-9485), mounted at the top of the content
 * column so the reminder banner pushes the page down instead of covering it:
 *
 * - day 1: a welcome card in the corner, for everyone;
 * - halfway, three days left and the last day: a dismissible banner for
 *   owners and admins;
 * - after Cloud confirms the end: a dialog for owners and admins, a toast for
 *   members.
 *
 * Each shows once per device. Nothing here decides access; the server does.
 */
export function WorkspaceTrialNotices() {
  const wsId = useCurrentWorkspace()?.id ?? "";
  const enabled = useFeatureEnabled(BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG, false);
  const trial = useWorkspaceTrial(wsId, enabled);
  const { role, isLoading } = useCurrentMember(wsId);
  // Who sees what depends on the role, and the notices are recorded as seen
  // per device: wait until the role is known rather than guess "member".
  if (!wsId || isLoading || role === null || trial.kind === "none") return null;
  const isManager = role === "owner" || role === "admin";

  if (trial.kind === "active") {
    return (
      <>
        {isManager && trial.reminder !== null ? (
          <TrialReminderBanner key={trial.reminder} wsId={wsId} trial={trial} />
        ) : null}
        {trial.day === 1 ? <TrialWelcomeCard wsId={wsId} trial={trial} /> : null}
      </>
    );
  }
  // Announce the downgrade only once Cloud confirms it: when this clock
  // passes the end, the workspace may already have subscribed elsewhere.
  if (!trial.recent || !trial.confirmed) return null;
  if (!isManager) return <TrialEndedToast wsId={wsId} trial={trial} />;
  // The dialog lists what is restricted now; until the end has settled, the
  // server may still answer with the trial's Business policy.
  return trial.settled ? <TrialEndedDialog wsId={wsId} trial={trial} /> : null;
}

function useOpenBilling() {
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  return () => navigation.push(`${paths.settings()}?tab=billing`);
}

function TrialReminderBanner({ wsId, trial }: { wsId: string; trial: ActiveTrial }) {
  const { t } = useT("billing");
  const locale = useLocale();
  const reminder = trial.reminder ?? 7;
  const [seen, markSeen] = useNoticeSeen(wsId, trial.endsAt, `reminder:${reminder}`);
  const openBilling = useOpenBilling();
  if (seen) return null;

  const title =
    reminder === 7
      ? t(($) => $.trial.reminder.halfway_title, { count: trial.daysLeft })
      : reminder === 3
        ? t(($) => $.trial.reminder.days_left_title, { count: trial.daysLeft })
        : t(($) => $.trial.reminder.last_day_title);

  return (
    <section
      role="status"
      data-slot="trial-reminder"
      className="mx-3 mt-3 flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-lg bg-warning/10 px-4 py-2.5 ring-1 ring-warning/30"
    >
      <Hourglass aria-hidden="true" className="size-4 shrink-0 text-warning" />
      <p className="min-w-0 flex-1 text-body">
        <span className="font-semibold">{title}</span>{" "}
        <span className="text-muted-foreground">
          {t(($) => $.trial.reminder.body, { date: formatDate(trial.endsAt, locale) })}
        </span>
      </p>
      <div className="flex shrink-0 items-center gap-2">
        <Button variant="ghost" size="sm" onClick={markSeen}>
          {t(($) => $.trial.reminder.later)}
        </Button>
        <Button
          size="sm"
          onClick={() => {
            markSeen();
            openBilling();
          }}
        >
          {t(($) => $.trial.reminder.choose_plan)}
        </Button>
      </div>
    </section>
  );
}

function TrialWelcomeCard({ wsId, trial }: { wsId: string; trial: ActiveTrial }) {
  const { t } = useT("billing");
  const titleId = useId();
  const [seen, markSeen] = useNoticeSeen(wsId, trial.endsAt, "welcome");
  const openBilling = useOpenBilling();
  const chatOpen = useChatStore((s) => s.isOpen);
  const floatingChatEnabled = useChatStore((s) => s.floatingChatEnabled);
  const { pathname } = useNavigation();
  const chatPath = useWorkspacePaths().chat();
  const corner = useSessionPromptStore((s) => s.corner);
  const launcherVisible =
    floatingChatEnabled && !chatOpen && !isFloatingChatRouteSuppressed(pathname, chatPath);

  // Claim the corner whenever it is free, including after another card that
  // held it closes; give it back once seen or unmounted.
  useEffect(() => {
    const store = useSessionPromptStore.getState();
    if (seen) store.releaseCorner("trial");
    else if (corner === null) store.claimCorner("trial");
  }, [corner, seen]);
  useEffect(() => () => useSessionPromptStore.getState().releaseCorner("trial"), []);

  // Another card holds the corner, or the chat window covers it.
  if (seen || corner !== "trial" || chatOpen) return null;

  return (
    <section
      aria-labelledby={titleId}
      aria-live="polite"
      data-slot="trial-welcome"
      className={cn(
        "absolute right-[var(--chat-launcher-inset)] z-50 w-[min(23.75rem,calc(100%-2*var(--chat-launcher-inset)))] rounded-xl bg-surface-raised p-4 pb-3 text-popover-foreground shadow-[var(--floating-shadow)] ring-1 ring-floating-border",
        "animate-in fade-in-0 slide-in-from-bottom-2 duration-200 motion-reduce:animate-none",
        launcherVisible ? "above-chat-launcher" : "bottom-[var(--chat-launcher-inset)]",
      )}
    >
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={t(($) => $.trial.welcome.close)}
        onClick={markSeen}
        className="absolute top-2 right-2 text-muted-foreground"
      >
        <X />
      </Button>
      <div className="flex items-start gap-2 pr-6">
        <Sparkles aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-brand" />
        <div className="min-w-0">
          <h2 id={titleId} className="text-body font-semibold">
            {t(($) => $.trial.welcome.title, { count: trial.totalDays })}
          </h2>
          <p className="mt-1 text-label text-muted-foreground">{t(($) => $.trial.welcome.body)}</p>
        </div>
      </div>
      <div className="mt-3 flex justify-end">
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            markSeen();
            openBilling();
          }}
        >
          {t(($) => $.trial.welcome.action)}
        </Button>
      </div>
    </section>
  );
}

/**
 * Lists only what is restricted right now. While limits are still observed
 * rather than enforced, the end of the trial blocks nothing, and the dialog
 * must not say otherwise.
 */
function TrialEndedDialog({ wsId, trial }: { wsId: string; trial: EndedTrial }) {
  const { t } = useT("billing");
  const locale = useLocale();
  const workspaceName = useCurrentWorkspace()?.name ?? "";
  const [seen, markSeen] = useNoticeSeen(wsId, trial.endsAt, "ended");
  const openBilling = useOpenBilling();
  // Read both again on mount and use only answers received after the end
  // settled, so a cached Business answer never leaves a restriction out. The
  // host renders this only once settled, so settledAt is set.
  const settledAt = trial.settledAt ?? Number.POSITIVE_INFINITY;
  const featuresQuery = useQuery({
    ...workspaceFeaturesOptions(wsId),
    enabled: !seen,
    refetchOnMount: "always",
  });
  const usageQuery = useQuery({
    ...autopilotQuotaUsageOptions(wsId),
    enabled: !seen,
    refetchOnMount: "always",
  });
  const current = (query: { isPending: boolean; dataUpdatedAt: number }) =>
    !query.isPending && query.dataUpdatedAt >= settledAt;
  // Cloud has confirmed the downgrade; the details only add to it. A detail
  // read that failed does not hold the dialog back: it opens without that
  // line, which appears if a later read succeeds while it is open.
  const answered = (query: {
    isPending: boolean;
    isError: boolean;
    isFetching: boolean;
    dataUpdatedAt: number;
  }) => current(query) || (query.isError && !query.isFetching);
  const ready = answered(featuresQuery) && answered(usageQuery);
  // Once open, it stays open while a detail read retries (on refocus, say);
  // the retry only fills in its line.
  const [opened, setOpened] = useState(false);
  useEffect(() => {
    if (ready) setOpened(true);
  }, [ready]);
  if (seen || !(opened || ready)) return null;

  const count = new Intl.NumberFormat(locale);
  const privateProjectsLocked =
    current(featuresQuery) && featuresQuery.data?.features?.private_projects === "locked";
  const usage = current(usageQuery) ? usageQuery.data : undefined;
  const autopilotEnforced =
    usage?.action === "enforce" && usage.limit !== null && usage.used !== null;

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : markSeen())}>
      <DialogContent data-slot="trial-ended">
        <DialogHeader>
          <DialogTitle>{t(($) => $.trial.ended.title)}</DialogTitle>
          <DialogDescription>
            {workspaceName
              ? t(($) => $.trial.ended.description, { workspace: workspaceName })
              : t(($) => $.trial.ended.description_without_name)}
          </DialogDescription>
        </DialogHeader>
        <ul className="space-y-2.5 text-body">
          <li className="flex items-start gap-2.5">
            <Users aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <span>{t(($) => $.trial.ended.kept)}</span>
          </li>
          {privateProjectsLocked ? (
            <li className="flex items-start gap-2.5">
              <Lock aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
              <span>{t(($) => $.trial.ended.private_projects)}</span>
            </li>
          ) : null}
          {autopilotEnforced && usage ? (
            <li className="flex items-start gap-2.5">
              <Bot aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
              <span>
                {usage.reached && usage.reset_at
                  ? t(($) => $.trial.ended.autopilot_paused, {
                      limit: count.format(usage.limit ?? 0),
                      used: count.format(usage.used ?? 0),
                      date: formatDate(Date.parse(usage.reset_at ?? ""), locale),
                    })
                  : t(($) => $.trial.ended.autopilot, {
                      limit: count.format(usage.limit ?? 0),
                      used: count.format(usage.used ?? 0),
                    })}
              </span>
            </li>
          ) : null}
        </ul>
        <DialogFooter>
          <Button variant="outline" onClick={markSeen}>
            {t(($) => $.trial.ended.keep_free)}
          </Button>
          <Button
            onClick={() => {
              markSeen();
              openBilling();
            }}
          >
            {t(($) => $.trial.ended.upgrade)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TrialEndedToast({ wsId, trial }: { wsId: string; trial: EndedTrial }) {
  const { t } = useT("billing");
  const [seen, markSeen] = useNoticeSeen(wsId, trial.endsAt, "ended");
  // Per trial, not per mount: switching workspaces keeps this mounted.
  const key = trialNoticeKey(wsId, trial.endsAt, "ended");
  const shownFor = useRef<string | null>(null);

  useEffect(() => {
    if (seen || shownFor.current === key) return;
    shownFor.current = key;
    markSeen();
    toast(t(($) => $.trial.ended.member_title), {
      description: t(($) => $.trial.ended.member_description),
    });
  }, [key, markSeen, seen, t]);

  return null;
}
