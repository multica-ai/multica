// CEREBRO: FIR-1714 — pending wakeups folded INTO the running-agent activity bar
// (AgentLiveCard) instead of sitting as a second, separate banner. When an issue
// has both a working agent and a scheduled run, the existing multi-collapse on
// that bar now folds the wakeup in as one more row, so the two read as one bar.
// This file owns the reusable pieces the upstream bar consumes: the query hook
// (so the bar knows the issue's pending wakeups + a live tick) and the row
// renderer (so a wakeup row looks identical wherever it is shown). Pure English
// copy — the app UI is English (Jesper FIR-1521).
"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlarmClock, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { type Wakeup, sortWakeups, wakeupLabel, wakeupCounter } from "../lib/wakeup-format";

// Shared cache key across every wakeup surface (bar, pip, section) so a cancel
// from any of them refreshes all of them.
const WAKEUP_QUERY_KEY = (issueId: string) => ["cerebro-wakeups", issueId];

export interface IssueWakeups {
  wakeups: Wakeup[];
  now: number;
  cancel: (id: string) => void;
  cancellingIds: ReadonlySet<string>;
}

// Fetches an issue's pending wakeups and keeps a 1s tick alive while any exist,
// so countdowns stay live. `enabled` lets the caller honour the cerebro_wakeup_bar
// flag without making the hook call conditional.
export function useIssueWakeups(issueId: string, enabled = true): IssueWakeups {
  const queryClient = useQueryClient();
  const [now, setNow] = useState(() => Date.now());
  const [cancellingIds, setCancellingIds] = useState<ReadonlySet<string>>(() => new Set());

  const { data } = useQuery({
    queryKey: WAKEUP_QUERY_KEY(issueId),
    queryFn: () => api.listIssueWakeups(issueId),
    staleTime: 30_000,
    enabled,
  });

  const wakeups = useMemo(
    () => (enabled ? sortWakeups((data?.wakeups ?? []) as Wakeup[]) : []),
    [data, enabled],
  );

  // Tick once a second only while a wakeup is shown, so we never run a timer on
  // an issue that has none.
  useEffect(() => {
    if (wakeups.length === 0) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [wakeups.length]);

  async function cancel(id: string) {
    setCancellingIds((prev) => new Set(prev).add(id));
    try {
      await api.cancelWakeup(id);
      await queryClient.invalidateQueries({ queryKey: WAKEUP_QUERY_KEY(issueId) });
    } catch {
      toast.error("Couldn't cancel scheduled run");
    } finally {
      setCancellingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }

  return { wakeups, now, cancel, cancellingIds };
}

// One pending-wakeup row: alarm-clock + type label + live countdown + prompt +
// Cancel. Used inside AgentLiveCard's single banner and its expanded multi list,
// so the orange wakeup row reads identically to the running-agent rows above it.
export function WakeupActivityRow({
  wakeup,
  now,
  onCancel,
  cancelling,
}: {
  wakeup: Wakeup;
  now: number;
  onCancel: () => void;
  cancelling: boolean;
}) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 text-muted-foreground">
      <AlarmClock className="h-3.5 w-3.5 shrink-0 text-warning" />
      <div className="flex items-center gap-1.5 text-xs min-w-0">
        {/* Type + countdown never shrink — only the prompt truncates, so the
            wakeup type label stays readable (Jesper FIR-1521). */}
        <span className="font-medium text-foreground shrink-0">{wakeupLabel(wakeup)}</span>
        <span className="text-warning tabular-nums shrink-0">{wakeupCounter(wakeup, now)}</span>
        {wakeup.prompt && (
          <span className="text-muted-foreground truncate min-w-0" title={wakeup.prompt}>
            {wakeup.prompt}
          </span>
        )}
      </div>
      <div className="ml-auto flex items-center shrink-0">
        <button
          type="button"
          onClick={onCancel}
          disabled={cancelling}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
          title="Cancel scheduled run"
        >
          <X className="h-3 w-3" />
          <span>Cancel</span>
        </button>
      </div>
    </div>
  );
}
