import { useEffect, useState } from "react";
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
 * How long after the end the app refetches once more. The server caches
 * Cloud's enforcement policy for up to a minute and the viewer's clock can be
 * off from Cloud's, so a refetch exactly at the end can still read the trial's
 * Business policy.
 */
export const TRIAL_END_SETTLE_MS = 70 * 1000;

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
       * Past TRIAL_END_SETTLE_MS after the end: paid-feature states and limits
       * read from now on are Free's.
       */
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
  return {
    kind: "ended",
    plan: trial.plan,
    endsAt,
    recent: now - endsAt < TRIAL_ENDED_VISIBLE_DAYS * DAY_MS,
    settled: now - endsAt >= TRIAL_END_SETTLE_MS,
  };
}

/**
 * Re-render often enough for day counts, and exactly when the trial ends.
 */
const TRIAL_CLOCK_TICK_MS = 60 * 60 * 1000;

/**
 * The trial view for a workspace, kept current: it re-renders hourly for the
 * day count and, when the trial ends, refetches everything the end changes
 * (plan, paid-feature states, autopilot limit) instead of waiting for their
 * stale times: once at the end, and once more TRIAL_END_SETTLE_MS later.
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
    // The shell reads this on every page; the Billing page keeps its own
    // shorter refresh.
    staleTime: 5 * 60 * 1000,
  });
  const [now, setNow] = useState(() => Date.now());
  const summary = enabled ? summaryQuery.data : null;
  // Keyed on the end, not on the state, so the follow-up refetch survives the
  // summary turning from active to expired at the first one.
  const endsAtRaw =
    summary?.trial && summary.trial.state !== "converted"
      ? summary.trial.endsAt
      : null;

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TRIAL_CLOCK_TICK_MS);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    if (!endsAtRaw || !wsId) return;
    const endsAt = Date.parse(endsAtRaw);
    if (Number.isNaN(endsAt)) return;
    const refresh = () => {
      setNow(Date.now());
      void qc.invalidateQueries({ queryKey: workspaceSubscriptionKeys.all(wsId) });
      void qc.invalidateQueries({ queryKey: workspaceFeatureKeys.all(wsId) });
      void qc.invalidateQueries({ queryKey: autopilotKeys.usage(wsId) });
    };
    const timers: number[] = [];
    for (const at of [endsAt, endsAt + TRIAL_END_SETTLE_MS]) {
      const delay = at - Date.now();
      // A boundary already passed needs nothing: reads made after it are
      // current. setTimeout overflows past ~24.8 days, far beyond a trial.
      if (delay < 0 || delay > 2 ** 31 - 1) continue;
      timers.push(window.setTimeout(refresh, delay));
    }
    return () => timers.forEach((id) => window.clearTimeout(id));
  }, [endsAtRaw, qc, wsId]);

  return resolveWorkspaceTrial(summary, now);
}
