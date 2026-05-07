"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentActivityBucket } from "../types";
import { agentListOptions } from "../workspace/queries";
import { agentActivity30dOptions } from "./queries";

const DAYS = 30;
const DAY_MS = 24 * 60 * 60 * 1000;

/** One day's tally for the sparkline. */
export interface ActivityBucket {
  total: number;
  failed: number;
}

export interface AgentActivity {
  /**
   * 30 daily buckets, oldest → newest. Days with no activity are
   * zero-filled. Each surface picks how much of the tail to render: the
   * Agents list uses 7, the agent detail uses all 30. Reading is the
   * caller's job (see `summarizeActivityWindow` for the standard
   * tail-slice + roll-up).
   */
  buckets: ActivityBucket[];
  /**
   * Days the agent has existed, capped at DAYS. Pure cosmetic — used by
   * tooltip copy ("Created 3 days ago"). The sparkline doesn't change
   * shape for young agents on purpose; pre-life days look the same as
   * zero days.
   */
  daysSinceCreated: number;
}

/**
 * Window-sized roll-up of an agent's activity series. Both the Agents
 * list (windowDays=7) and the detail "Last 30 days" panel (windowDays=30)
 * read through this so the totals can never drift from the bars they
 * label.
 */
export interface ActivityWindowSummary {
  /** Trailing-N buckets from the activity series (newest end). */
  buckets: ActivityBucket[];
  /** Sum of `bucket.total` across the window. */
  totalRuns: number;
  /** Sum of `bucket.failed` across the window. */
  totalFailed: number;
  /** Echo of the input window — the renderer uses it for copy. */
  windowDays: number;
}

const EMPTY: AgentActivity = {
  buckets: Array.from({ length: DAYS }, () => ({ total: 0, failed: 0 })),
  daysSinceCreated: DAYS,
};

const EMPTY_SUMMARY: ActivityWindowSummary = {
  buckets: [],
  totalRuns: 0,
  totalFailed: 0,
  windowDays: 0,
};

/**
 * Workspace-wide activity map keyed by `agent.id`. Single-pass batch:
 * one fetch + one derivation pass backs every row's sparkline on the
 * list AND the detail panel — adding rows costs O(1) HTTP and O(N)
 * compute (not O(N) HTTP).
 */
export function useWorkspaceActivityMap(wsId: string | undefined): {
  byAgent: Map<string, AgentActivity>;
  loading: boolean;
} {
  const { data: agents, isPending: agentsPending } = useQuery({
    ...agentListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: buckets, isPending: bucketsPending } = useQuery({
    ...agentActivity30dOptions(wsId ?? ""),
    enabled: !!wsId,
  });

  const byAgent = useMemo(() => {
    if (!agents || !buckets) return new Map<string, AgentActivity>();
    return buildActivityMap(agents, buckets, Date.now());
  }, [agents, buckets]);

  return { byAgent, loading: agentsPending || bucketsPending };
}

export function buildActivityMap(
  agents: readonly Agent[],
  buckets: readonly AgentActivityBucket[],
  now: number,
): Map<string, AgentActivity> {
  // Group buckets by agent once so per-agent derivation is O(buckets) not
  // O(agents × buckets).
  const bucketsByAgent = new Map<string, AgentActivityBucket[]>();
  for (const b of buckets) {
    const list = bucketsByAgent.get(b.agent_id);
    if (list) list.push(b);
    else bucketsByAgent.set(b.agent_id, [b]);
  }

  const out = new Map<string, AgentActivity>();
  for (const agent of agents) {
    out.set(
      agent.id,
      deriveAgentActivity(
        bucketsByAgent.get(agent.id) ?? [],
        agent.created_at,
        now,
      ),
    );
  }
  return out;
}

/**
 * Pure derivation: filter the workspace-wide buckets to one agent and
 * normalise to a fixed 30-element series ending at `now`. Exported for
 * unit-testing and direct reuse on surfaces that already have the
 * workspace-wide buckets in hand.
 */
export function deriveAgentActivity(
  buckets: readonly AgentActivityBucket[],
  agentCreatedAt: string,
  now: number,
): AgentActivity {
  const series: ActivityBucket[] = Array.from({ length: DAYS }, () => ({
    total: 0,
    failed: 0,
  }));

  // Newest slot is the start of "today" in local time; we walk back DAYS
  // slots so index 0 = oldest, index DAYS-1 = today.
  const today = startOfDay(now);

  for (const b of buckets) {
    const ts = new Date(b.bucket_at).getTime();
    if (Number.isNaN(ts)) continue;
    const daysAgo = Math.floor((today - startOfDay(ts)) / DAY_MS);
    if (daysAgo < 0 || daysAgo >= DAYS) continue;
    const slot = DAYS - 1 - daysAgo;
    series[slot]!.total += b.task_count;
    series[slot]!.failed += b.failed_count;
  }

  const createdAt = new Date(agentCreatedAt).getTime();
  const ageMs = Number.isFinite(createdAt) ? now - createdAt : Infinity;
  const daysSinceCreated = Math.min(
    DAYS,
    Math.max(0, Math.floor(ageMs / DAY_MS)),
  );

  return {
    buckets: series,
    daysSinceCreated,
  };
}

/**
 * Take the trailing N buckets and roll up totals over them. This is the
 * single entry point both surfaces (list + detail) read through, so the
 * numbers can never disagree with the bars they label.
 *
 * `windowDays` is clamped to the available bucket count, so passing a
 * value larger than `activity.buckets.length` returns the full series
 * rather than an out-of-range slice.
 */
export function summarizeActivityWindow(
  activity: AgentActivity | undefined,
  windowDays: number,
): ActivityWindowSummary {
  if (!activity) return { ...EMPTY_SUMMARY, windowDays };
  const safeWindow = Math.min(
    Math.max(0, windowDays),
    activity.buckets.length,
  );
  // `slice(-0)` returns the full array (JS quirk: -0 === 0), so guard
  // explicitly when no window is requested.
  const slice =
    safeWindow === 0 ? [] : activity.buckets.slice(-safeWindow);
  let totalRuns = 0;
  let totalFailed = 0;
  for (const b of slice) {
    totalRuns += b.total;
    totalFailed += b.failed;
  }
  return { buckets: slice, totalRuns, totalFailed, windowDays };
}

function startOfDay(ts: number): number {
  // Local-time day boundary. The back-end truncates to UTC midnight, but
  // the user's mental model is "today/yesterday in the timezone they're
  // looking at"; using local matches that and keeps "today" stable across
  // a working session even when buckets cross UTC midnight.
  const d = new Date(ts);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

export const __EMPTY_ACTIVITY = EMPTY;
