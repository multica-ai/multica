import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { WorkspaceSubscriptionSummary } from "../types";
import { autopilotKeys } from "../autopilots/queries";
import { workspaceFeatureKeys } from "./paid-features";
import {
  workspaceSubscriptionKeys,
  workspaceSubscriptionSummaryOptions,
} from "./workspace-subscription-queries";

/**
 * The no-card Business trial (MUL-9485), as the app shell shows it.
 *
 * Cloud decides what the workspace is entitled to; this module only turns the
 * trial's timestamps into what to show today. Days are counted from `endsAt`
 * in whole 24-hour steps, so "3 days left" means less than 72 hours remain,
 * whatever the viewer's timezone.
 */

export const DAY_MS = 24 * 60 * 60 * 1000;

/** The status bar turns to a warning for the last three days. */
export const TRIAL_WARNING_DAYS = 3;

/** After the trial ends, the status bar offers an upgrade for a week. */
export const TRIAL_ENDED_VISIBLE_DAYS = 7;

/**
 * How long after the end the app refetches once more. The server may cache
 * Cloud's enforcement policy for up to a minute, so a refetch exactly at the
 * end can still read the trial's Business policy.
 */
export const TRIAL_END_SETTLE_MS = 70 * 1000;

/**
 * How long after Cloud first confirms the end paid-feature states are read
 * again. Cloud stops serving the trial's policy at the end, give or take a
 * second; this covers that and the viewer's clock being behind Cloud's.
 */
export const TRIAL_CONFIRM_SETTLE_MS = 5 * 1000;

/**
 * While this clock says the trial has ended but Cloud still reports it
 * active (this clock runs ahead), the summary is asked again after these
 * delays, the last one repeating.
 */
export const TRIAL_END_RECHECK_MS = [30_000, 60_000, 120_000, 300_000] as const;

/**
 * Owners and admins get one dismissible reminder at each of these points:
 * halfway, three days left, and the last day.
 */
export const TRIAL_REMINDER_MILESTONES = [7, 3, 1] as const;
export type TrialReminderMilestone = (typeof TRIAL_REMINDER_MILESTONES)[number];

export type WorkspaceTrialView =
  | { kind: "none" }
  | {
      kind: "active";
      plan: string;
      startedAt: number;
      endsAt: number;
      /** Whole days left, rounded up: 1 on the last day. */
      daysLeft: number;
      /** The current day of the trial, 1-based. */
      day: number;
      totalDays: number;
      warning: boolean;
      /** The reminder due now, if any; the caller tracks which were seen. */
      reminder: TrialReminderMilestone | null;
    }
  | {
      kind: "ended";
      plan: string;
      endsAt: number;
      /** Within the week after the end, while the status bar still shows it. */
      recent: boolean;
      /**
       * Cloud has said the trial expired and the workspace is on Free. Until
       * then the end is only this clock's: the workspace may have subscribed
       * elsewhere, so nothing may announce a downgrade yet.
       */
      confirmed: boolean;
      /**
       * From when paid-feature states and limits read are Free's: the later of
       * TRIAL_END_SETTLE_MS after the end and TRIAL_CONFIRM_SETTLE_MS after
       * Cloud's confirmation was received. Null until confirmed.
       */
      settledAt: number | null;
      /** Confirmed, and past `settledAt`. */
      settled: boolean;
    };

/**
 * What to show for the workspace's trial at `now`. A summary that still says
 * `active` after `endsAt` is stale: it reads as ended so the shell flips at
 * the boundary while the refetch catches up.
 */
export function resolveWorkspaceTrial(
  summary: WorkspaceSubscriptionSummary | null | undefined,
  now: number,
  /** When this client received Cloud's confirmation of the end, if known. */
  confirmedAt?: number,
): WorkspaceTrialView {
  const trial = summary?.trial;
  if (!trial || trial.state === "converted") return { kind: "none" };
  const startedAt = Date.parse(trial.startedAt);
  const endsAt = Date.parse(trial.endsAt);
  if (Number.isNaN(startedAt) || Number.isNaN(endsAt) || endsAt <= startedAt) {
    return { kind: "none" };
  }
  if (trial.state === "active" && now < endsAt) {
    const totalDays = Math.max(1, Math.round((endsAt - startedAt) / DAY_MS));
    const daysLeft = Math.max(1, Math.ceil((endsAt - now) / DAY_MS));
    const day = Math.min(
      totalDays,
      Math.max(1, Math.floor((now - startedAt) / DAY_MS) + 1),
    );
    const reminder =
      [...TRIAL_REMINDER_MILESTONES]
        .reverse()
        .find((milestone) => daysLeft <= milestone && milestone < totalDays) ??
      null;
    return {
      kind: "active",
      plan: trial.plan,
      startedAt,
      endsAt,
      daysLeft,
      day,
      totalDays,
      warning: daysLeft <= TRIAL_WARNING_DAYS,
      reminder,
    };
  }
  // A paid plan in effect means the trial no longer matters to the shell.
  if (summary.entitlement.plan !== "free" && trial.state !== "active") {
    return { kind: "none" };
  }
  const confirmed = trial.state === "expired" && summary.entitlement.plan === "free";
  const settledAt = confirmed
    ? Math.max(
        endsAt + TRIAL_END_SETTLE_MS,
        (confirmedAt ?? endsAt) + TRIAL_CONFIRM_SETTLE_MS,
      )
    : null;
  return {
    kind: "ended",
    plan: trial.plan,
    endsAt,
    recent: now - endsAt < TRIAL_ENDED_VISIBLE_DAYS * DAY_MS,
    confirmed,
    settledAt,
    settled: settledAt !== null && now >= settledAt,
  };
}

/**
 * After refetching paid-feature reads made before a plan change settled, a
 * read that is still failing and on screen is asked again after these
 * delays; then it is left to the next focus or mount.
 */
export const PAID_READ_RETRY_MS = [30_000, 60_000, 120_000, 300_000] as const;

type QueryClientLike = ReturnType<typeof useQueryClient>;

/**
 * Refetch the workspace's paid-feature reads (feature states, autopilot
 * usage) that were last answered before `boundary`, including ones that never
 * answered at all, and keep retrying with backoff while one shown on screen
 * still fails. Reads answered after `boundary` are current and left alone.
 */
async function refreshPaidReadsBefore(
  qc: QueryClientLike,
  wsId: string,
  boundary: number,
  wait: (ms: number) => Promise<void>,
  isCancelled: () => boolean,
): Promise<void> {
  const keys = [workspaceFeatureKeys.all(wsId), autopilotKeys.usage(wsId)];
  const outdated = (query: { state: { dataUpdatedAt: number } }) =>
    query.state.dataUpdatedAt < boundary;
  for (let attempt = 0; ; attempt++) {
    // A read already in flight is awaited rather than restarted, so one that
    // goes on to fail is still caught by the check below.
    await Promise.all(
      keys.map((queryKey) =>
        qc.invalidateQueries({ queryKey, predicate: outdated }, { cancelRefetch: false }),
      ),
    );
    if (isCancelled()) return;
    const failing = keys.some((queryKey) =>
      qc
        .getQueryCache()
        .findAll({ queryKey })
        .some(
          (query) =>
            query.state.status === "error" &&
            query.getObserversCount() > 0 &&
            query.state.dataUpdatedAt < boundary,
        ),
    );
    if (!failing || attempt >= PAID_READ_RETRY_MS.length) return;
    await wait(PAID_READ_RETRY_MS[attempt]!);
    if (isCancelled()) return;
  }
}

/**
 * Re-render often enough for day counts, and exactly when the trial ends.
 */
const TRIAL_CLOCK_TICK_MS = 60 * 60 * 1000;

/**
 * The trial view for a workspace, kept current. It re-renders hourly for the
 * day count and, around the end, refetches everything the end changes (plan,
 * paid-feature states, autopilot limit) instead of waiting for their stale
 * times:
 *
 * - at the end on this clock, and TRIAL_END_SETTLE_MS later;
 * - while Cloud still reports the trial active past that point (this clock
 *   runs ahead of Cloud's), the summary again with backoff until Cloud
 *   answers;
 * - once Cloud confirms the end, at the view's `settledAt`.
 *
 * A boundary that passed while the workspace was not showing (another desktop
 * tab's workspace, a closed window) is caught up when it shows again.
 *
 * `enabled` is the caller's billing gate; a self-hosted deployment never
 * requests the summary.
 */
export function useWorkspaceTrial(
  wsId: string,
  enabled: boolean,
): WorkspaceTrialView {
  const qc = useQueryClient();
  const summaryQuery = useQuery({
    ...workspaceSubscriptionSummaryOptions(wsId),
    enabled: enabled && wsId.length > 0,
    // During a trial, coming back to the tab always re-reads the plan, even
    // inside the stale time: it may have been bought on another device.
    refetchOnWindowFocus: (query) =>
      query.state.data?.trial?.state === "active" ? "always" : true,
  });
  const [now, setNow] = useState(() => Date.now());
  const summary = enabled ? summaryQuery.data : null;
  // Keyed on the end, not on the state, so the follow-up refetch survives the
  // summary turning from active to expired at the first one.
  const endsAtRaw =
    summary?.trial && summary.trial.state !== "converted"
      ? summary.trial.endsAt
      : null;
  const trialKey = endsAtRaw ? `${wsId}:${endsAtRaw}` : null;
  // The last trial end this workspace showed. The end's refetches keep running
  // when the summary stops carrying the trial (it converted to a paid plan on
  // another device): reads made just before the end may still hold the
  // trial's Business features.
  //
  // This and the records below are kept per workspace: a desktop window
  // switches workspaces without remounting, and switching back must pick up
  // the refetches the other workspace's trial would otherwise have replaced.
  const [lastEnds, setLastEnds] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!endsAtRaw || !wsId) return;
    setLastEnds((current) =>
      current[wsId] === endsAtRaw ? current : { ...current, [wsId]: endsAtRaw },
    );
  }, [endsAtRaw, wsId]);
  const lastEnd = lastEnds[wsId] ?? null;
  const boundaryEndRaw = endsAtRaw ?? lastEnd;

  // When this client first saw the workspace's current plan, per workspace.
  // Any plan change (subscribing mid-trial, possibly on another device) and a
  // first read of the plan (opening the app just after one) may be followed
  // by paid-feature reads the server answered from its cached previous policy
  // for up to a minute, so those reads are refreshed once that has passed.
  const planKey = summary
    ? `${summary.entitlement.plan}|${summary.entitlement.status}|${summary.trial?.state ?? "-"}`
    : null;
  const [planSeen, setPlanSeen] = useState<Record<string, { plan: string; at: number }>>({});
  const planSeenAt = planKey && planSeen[wsId]?.plan === planKey ? planSeen[wsId].at : undefined;
  useEffect(() => {
    if (!planKey || !wsId) return;
    setPlanSeen((current) =>
      current[wsId]?.plan === planKey
        ? current
        : { ...current, [wsId]: { plan: planKey, at: summaryQuery.dataUpdatedAt || Date.now() } },
    );
  }, [planKey, summaryQuery.dataUpdatedAt, wsId]);
  useEffect(() => {
    if (planSeenAt === undefined || !wsId) return;
    const boundary = planSeenAt + TRIAL_END_SETTLE_MS;
    let cancelled = false;
    let timer: number | undefined;
    const wait = (ms: number) =>
      new Promise<void>((resolve) => {
        timer = window.setTimeout(resolve, ms);
      });
    const run = async () => {
      const delay = boundary - Date.now();
      // A boundary that passed while this workspace was not showing is caught
      // up now.
      if (delay > 0) await wait(delay);
      if (!cancelled) {
        await refreshPaidReadsBefore(qc, wsId, boundary, wait, () => cancelled);
      }
    };
    void run();
    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [planSeenAt, qc, wsId]);
  const cloudSaysActive = summary?.trial?.state === "active";
  const cloudConfirmsEnd =
    summary?.trial?.state === "expired" && summary.entitlement.plan === "free";

  // When this client first received Cloud's confirmation, per trial.
  const [confirmations, setConfirmations] = useState<Record<string, number>>({});
  const confirmedAt = trialKey ? confirmations[trialKey] : undefined;
  useEffect(() => {
    if (!cloudConfirmsEnd || !trialKey) return;
    setConfirmations((current) =>
      trialKey in current
        ? current
        : { ...current, [trialKey]: summaryQuery.dataUpdatedAt || Date.now() },
    );
  }, [cloudConfirmsEnd, summaryQuery.dataUpdatedAt, trialKey]);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TRIAL_CLOCK_TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    // A workspace switch reuses this hook: count from now, not from the
    // previous workspace's last tick.
    setNow(Date.now());
    if (!boundaryEndRaw || !wsId) return;
    const endsAt = Date.parse(boundaryEndRaw);
    if (Number.isNaN(endsAt)) return;
    const keys = [
      workspaceSubscriptionKeys.all(wsId),
      workspaceFeatureKeys.all(wsId),
      autopilotKeys.usage(wsId),
    ];
    // Refetch what was read before `boundary`; reads made after it are current.
    const refreshReadsBefore = (boundary: number, queryKeys = keys) => {
      setNow(Date.now());
      for (const queryKey of queryKeys) {
        void qc.invalidateQueries({
          queryKey,
          predicate: (query) =>
            query.state.dataUpdatedAt > 0 && query.state.dataUpdatedAt < boundary,
        });
      }
    };
    const boundaries: Array<{ at: number; keys: typeof keys }> = [
      { at: endsAt, keys },
      { at: endsAt + TRIAL_END_SETTLE_MS, keys },
    ];
    if (confirmedAt !== undefined) {
      // The summary is already Cloud's answer; only policy-backed reads lag.
      boundaries.push({ at: confirmedAt + TRIAL_CONFIRM_SETTLE_MS, keys: keys.slice(1) });
    }
    const timers: number[] = [];
    for (const boundary of boundaries) {
      const delay = boundary.at - Date.now();
      if (delay <= 0) {
        // Passed while this workspace was not showing: catch up now.
        refreshReadsBefore(boundary.at, boundary.keys);
      } else if (delay <= 2 ** 31 - 1) {
        // setTimeout overflows past ~24.8 days, far beyond a trial.
        timers.push(
          window.setTimeout(() => refreshReadsBefore(boundary.at, boundary.keys), delay),
        );
      }
    }
    return () => timers.forEach((id) => window.clearTimeout(id));
  }, [boundaryEndRaw, confirmedAt, qc, wsId]);

  // This clock is past the end and its follow-up, yet Cloud still says
  // active: ask again with backoff until Cloud answers otherwise.
  const recheckAttempt = useRef<{ key: string; attempt: number } | null>(null);
  useEffect(() => {
    if (!cloudSaysActive || !endsAtRaw || !trialKey) {
      recheckAttempt.current = null;
      return;
    }
    const endsAt = Date.parse(endsAtRaw);
    if (Number.isNaN(endsAt)) return;
    const recheckFrom = endsAt + TRIAL_END_SETTLE_MS;
    if (Date.now() < recheckFrom) return;
    const attempt =
      recheckAttempt.current?.key === trialKey ? recheckAttempt.current.attempt : 0;
    recheckAttempt.current = { key: trialKey, attempt: attempt + 1 };
    const delay =
      TRIAL_END_RECHECK_MS[Math.min(attempt, TRIAL_END_RECHECK_MS.length - 1)]!;
    const id = window.setTimeout(() => {
      void qc.invalidateQueries({ queryKey: workspaceSubscriptionKeys.all(wsId) });
    }, delay);
    return () => window.clearTimeout(id);
    // Each answer that is still active (a new dataUpdatedAt) and each failed
    // attempt (a new errorUpdatedAt; the last answer stays cached) re-arm it.
  }, [
    cloudSaysActive,
    endsAtRaw,
    qc,
    summaryQuery.dataUpdatedAt,
    summaryQuery.errorUpdatedAt,
    trialKey,
    wsId,
  ]);

  return resolveWorkspaceTrial(summary, now, confirmedAt);
}
