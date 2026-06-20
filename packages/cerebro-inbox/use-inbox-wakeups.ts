"use client";

// TECH-3322 — workspace-wide pending wakeups for the inbox. An issue with a
// scheduled wakeup has no in-flight task yet, so the upstream run-state queries
// don't surface it; this hook fills that gap so the inbox can drop such issues
// into the "Running" action group and mark their row with a scheduled-run dot
// (FIR-1521 — a dot like the running-job pip, not a clock; English copy only).
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";

/** One issue's nearest pending wakeup, ready for the row indicator. */
export interface InboxWakeupHint {
  /** RFC3339 fire time of the soonest time-trigger wakeup, if any. */
  fireAt?: string;
  /** Short "Scheduled run ~HH:MM" tooltip for the scheduled-run dot. */
  title: string;
}

// NOTE: the literal "cerebro-inbox-wakeups" key is mirrored in two upstream-zone
// realtime patches — CEREBRO-PATCH(inbox-wakeup-realtime) and
// CEREBRO-PATCH(reconnect-wakeup-invalidate) in
// packages/core/realtime/use-realtime-sync.ts — which invalidate it so a newly
// scheduled wakeup surfaces in the inbox without a manual refresh (FIR-1677).
// packages/core cannot import this package (dependency direction), so if you
// rename this key, update those two patches too.
const WAKEUP_QUERY_KEY = (wsId: string) => ["cerebro-inbox-wakeups", wsId] as const;

function formatClock(iso: string): string {
  try {
    // English locale; Copenhagen time so the displayed clock matches local time.
    return new Intl.DateTimeFormat("en-GB", {
      timeZone: "Europe/Copenhagen",
      weekday: "short",
      hour: "2-digit",
      minute: "2-digit",
    }).format(new Date(iso));
  } catch {
    return iso;
  }
}

/** Tooltip text for an issue's scheduled wakeup. English copy only (FIR-1521). */
function hintTitle(triggerType: string, fireAt?: string): string {
  if (triggerType === "time" && fireAt) return `Scheduled run ~${formatClock(fireAt)}`;
  if (triggerType === "issue_status") return "Scheduled: runs on status change";
  if (triggerType === "github_ci") return "Scheduled: runs on CI update";
  return "Scheduled run";
}

/**
 * Pure reducer over the wakeup list → one hint per issue, keeping the soonest
 * time-trigger (a time wakeup wins over a condition trigger so the row can show
 * a concrete clock). Exported for unit testing without a query client.
 */
export function buildInboxWakeupHints(
  wakeups: { issue_id: string; trigger_type: string; fire_at?: string }[],
): Map<string, InboxWakeupHint> {
  const map = new Map<string, InboxWakeupHint>();
  for (const w of wakeups) {
    if (!w.issue_id) continue;
    const existing = map.get(w.issue_id);
    const isTime = w.trigger_type === "time" && !!w.fire_at;
    // Prefer the earliest concrete time trigger; otherwise keep the first hint.
    if (existing) {
      const existingIsTime = !!existing.fireAt;
      if (!isTime) continue;
      if (existingIsTime && existing.fireAt! <= w.fire_at!) continue;
    }
    map.set(w.issue_id, { fireAt: isTime ? w.fire_at : undefined, title: hintTitle(w.trigger_type, w.fire_at) });
  }
  return map;
}

/**
 * Pending wakeups for the workspace, reduced to one hint per issue. `enabled`
 * gates the fetch behind the inbox-wakeup feature flag. Returns a stable empty
 * map when disabled or while loading.
 */
export function useInboxWakeupStates(
  wsId: string,
  enabled: boolean,
): Map<string, InboxWakeupHint> {
  const { data } = useQuery({
    queryKey: WAKEUP_QUERY_KEY(wsId),
    queryFn: () => api.listWorkspaceWakeups("pending"),
    enabled: enabled && !!wsId,
    staleTime: 30_000,
  });
  return useMemo(() => buildInboxWakeupHints(data?.wakeups ?? []), [data]);
}
