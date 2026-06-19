// CEREBRO: FIR-1521 (part 2) — a small orange "scheduled wakeup" clock pip for
// issue list rows and board cards. It stacks next to the running-agent indicator
// (IssueAgentActivityIndicator) so a row can show both "an agent is working" and
// "a run is scheduled" at the same time, overlapping like an avatar stack. Click
// opens a popover that lists every pending wakeup with a live countdown + cancel,
// matching the top-of-issue banner (CerebroWakeupBar). Mounted flag-gated
// (cerebro_activity_wakeup_dot) at the upstream call site, so this file stays free
// of any cerebro-feature-flags import (avoids the package cycle the banner notes).
"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlarmClock, X } from "lucide-react";
import { toast } from "sonner";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { api } from "@multica/core/api";
import { type Wakeup, sortWakeups, wakeupLabel, wakeupCounter } from "../lib/wakeup-format";

// Same cache key as CerebroWakeupBar / CerebroWakeupSection so a cancel from any
// surface refreshes the pip too.
const WAKEUP_QUERY_KEY = (issueId: string) => ["cerebro-wakeups", issueId];

function PipRow({
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
      <div className="flex min-w-0 flex-col gap-0.5 text-xs">
        <span className="flex items-center gap-1.5">
          <span className="truncate font-medium text-foreground">{wakeupLabel(wakeup)}</span>
          <span className="shrink-0 tabular-nums text-warning">{wakeupCounter(wakeup, now)}</span>
        </span>
        {wakeup.prompt && (
          <span className="truncate text-muted-foreground" title={wakeup.prompt}>
            {wakeup.prompt}
          </span>
        )}
      </div>
      <button
        type="button"
        onClick={onCancel}
        disabled={cancelling}
        className="ml-auto flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
        title="Annuller planlagt kørsel"
      >
        <X className="h-3 w-3" />
        <span>Annuller</span>
      </button>
    </div>
  );
}

export function CerebroIssueWakeupPip({
  issueId,
  className = "",
}: {
  issueId: string;
  className?: string;
}) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const [cancellingIds, setCancellingIds] = useState<ReadonlySet<string>>(() => new Set());

  const { data } = useQuery({
    queryKey: WAKEUP_QUERY_KEY(issueId),
    queryFn: () => api.listIssueWakeups(issueId),
    staleTime: 30_000,
  });

  const wakeups = useMemo(() => sortWakeups((data?.wakeups ?? []) as Wakeup[]), [data]);

  // Tick once a second only while the popover is open, so we never run a timer
  // for every row on a dense list — the closed pip is just a static clock.
  useEffect(() => {
    if (!open || wakeups.length === 0) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [open, wakeups.length]);

  async function handleCancel(id: string) {
    setCancellingIds((prev) => new Set(prev).add(id));
    try {
      await api.cancelWakeup(id);
      await queryClient.invalidateQueries({ queryKey: WAKEUP_QUERY_KEY(issueId) });
    } catch {
      toast.error("Kunne ikke annullere planlagt kørsel");
    } finally {
      setCancellingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }

  if (wakeups.length === 0) return null;

  const soonest = wakeups[0]!;
  const label =
    wakeups.length > 1
      ? `${wakeups.length} planlagte kørsler — næste ${wakeupCounter(soonest, now)}`
      : `${wakeupLabel(soonest)} ${wakeupCounter(soonest, now)}`;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label={label}
            title={label}
            // The pip lives inside the row's navigation link; stop the click from
            // following the link (and from selecting the row) when opening the popover.
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
            }}
            className={`relative inline-flex shrink-0 items-center text-warning outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-sm ${className}`}
          />
        }
      >
        <AlarmClock className="h-3.5 w-3.5" />
        {wakeups.length > 1 && (
          <span className="ml-0.5 text-[10px] font-medium leading-none tabular-nums">
            {wakeups.length}
          </span>
        )}
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className="w-72 overflow-hidden rounded-lg border border-warning/30 bg-warning/10 p-0"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="divide-y divide-warning/20">
          {wakeups.map((w) => (
            <PipRow
              key={w.id}
              wakeup={w}
              now={now}
              onCancel={() => handleCancel(w.id)}
              cancelling={cancellingIds.has(w.id)}
            />
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
