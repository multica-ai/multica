"use client";

// TECH-3322 — workspace-wide pending wakeups for the inbox. An issue with a
// scheduled wakeup has no in-flight task yet, so the upstream run-state queries
// don't surface it; this hook fills that gap so the inbox can drop such issues
// into the "Running" action group and mark their row with a clock.
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";

/** One issue's nearest pending wakeup, ready for the row indicator. */
export interface InboxWakeupHint {
  /** RFC3339 fire time of the soonest time-trigger wakeup, if any. */
  fireAt?: string;
  /** Short, localized "scheduled ~HH:MM" tooltip for the clock pip. */
  title: string;
}

const WAKEUP_QUERY_KEY = (wsId: string) => ["cerebro-inbox-wakeups", wsId] as const;

function formatClock(iso: string): string {
  try {
    return new Intl.DateTimeFormat("da-DK", {
      timeZone: "Europe/Copenhagen",
      weekday: "short",
      hour: "2-digit",
      minute: "2-digit",
    }).format(new Date(iso));
  } catch {
    return iso;
  }
}

/** Tooltip text for an issue's scheduled wakeup. "(cirka)" wording per TECH-3322. */
function hintTitle(triggerType: string, fireAt?: string): string {
  if (triggerType === "time" && fireAt) return `Planlagt kørsel ca. ${formatClock(fireAt)}`;
  if (triggerType === "issue_status") return "Planlagt: kører ved statusskift";
  if (triggerType === "github_ci") return "Planlagt: kører ved CI-opdatering";
  return "Planlagt kørsel";
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
