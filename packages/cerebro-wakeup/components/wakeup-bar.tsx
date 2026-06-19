// CEREBRO: FIR-1521 — prominent orange "scheduled wakeup" banner at the top of an
// issue's main column, mirroring the AgentLiveCard running banner. It answers
// "is this issue scheduled to run again, and when?" at a glance. Time triggers
// show a live countdown; issue_status / github_ci triggers show what they wait
// for plus how long they have been waiting. Multiple pending wakeups fold into
// one expandable banner, the same way the running card stacks multiple tasks.
"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlarmClock, ChevronDown, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";

// Shared with CerebroWakeupSection so a cancel from either surface refreshes both.
const WAKEUP_QUERY_KEY = (issueId: string) => ["cerebro-wakeups", issueId];

interface Wakeup {
  id: string;
  trigger_type: string;
  fire_at?: string;
  watch_status?: string;
  prompt: string;
  created_at: string;
}

// "om 2d 4t" / "om 14m" / "om 45s" / "når som helst" — counts down to fire time.
function formatCountdown(fireAt: string, now: number): string {
  const diff = new Date(fireAt).getTime() - now;
  if (diff <= 0 || Number.isNaN(diff)) return "når som helst";
  const s = Math.floor(diff / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `om ${d}d ${h}t`;
  if (h > 0) return `om ${h}t ${m}m`;
  if (m > 0) return `om ${m}m`;
  return `om ${sec}s`;
}

// "i 2t 14m" — how long a condition (status / CI) wakeup has been waiting.
function formatWaiting(createdAt: string, now: number): string {
  const s = Math.max(0, Math.floor((now - new Date(createdAt).getTime()) / 1000));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `i ${d}d ${h}t`;
  if (h > 0) return `i ${h}t ${m}m`;
  if (m > 0) return `i ${m}m`;
  return `i ${s}s`;
}

// Headline label for a wakeup, independent of the live counter.
function wakeupLabel(w: Wakeup): string {
  if (w.trigger_type === "issue_status" && w.watch_status)
    return `Venter på status: ${w.watch_status}`;
  if (w.trigger_type === "github_ci") return "Venter på CI";
  return "Planlagt kørsel";
}

// The ticking counter shown after the label: a countdown for time triggers,
// elapsed-waiting for condition triggers.
function wakeupCounter(w: Wakeup, now: number): string {
  if (w.trigger_type === "time" && w.fire_at) return formatCountdown(w.fire_at, now);
  return formatWaiting(w.created_at, now);
}

// Sort soonest-first: a concrete fire time wins over condition triggers, which
// fall back to oldest-waiting-first.
function sortWakeups(wakeups: Wakeup[]): Wakeup[] {
  return [...wakeups].sort((a, b) => {
    const at = a.trigger_type === "time" && a.fire_at ? new Date(a.fire_at).getTime() : Infinity;
    const bt = b.trigger_type === "time" && b.fire_at ? new Date(b.fire_at).getTime() : Infinity;
    if (at !== bt) return at - bt;
    return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
  });
}

function WakeupBarRow({
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
        <span className="font-medium text-foreground truncate">{wakeupLabel(wakeup)}</span>
        <span className="text-warning tabular-nums shrink-0">{wakeupCounter(wakeup, now)}</span>
        {wakeup.prompt && (
          <span className="text-muted-foreground truncate" title={wakeup.prompt}>
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
          title="Annuller planlagt kørsel"
        >
          <X className="h-3 w-3" />
          <span>Annuller</span>
        </button>
      </div>
    </div>
  );
}

// Gated at the mount site (issue-detail) behind the cerebro_wakeup_bar flag, so
// this component is free of any cerebro-feature-flags import — avoiding a package
// cycle (cerebro-feature-flags already depends on cerebro-wakeup for the limits UI).
export function CerebroWakeupBar({ issueId }: { issueId: string }) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const [cancellingIds, setCancellingIds] = useState<ReadonlySet<string>>(() => new Set());

  const { data } = useQuery({
    queryKey: WAKEUP_QUERY_KEY(issueId),
    queryFn: () => api.listIssueWakeups(issueId),
    staleTime: 30_000,
  });

  const wakeups = useMemo(() => sortWakeups((data?.wakeups ?? []) as Wakeup[]), [data]);

  // Tick once a second only while a banner is shown, so the countdown stays live
  // without a timer running on every issue that has no wakeups.
  useEffect(() => {
    if (wakeups.length === 0) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [wakeups.length]);

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

  const isMulti = wakeups.length > 1;
  const soonest = wakeups[0]!;

  return (
    // Orange banner directly under the running card. Not sticky — the running
    // card owns the sticky slot; this sits right beneath it so the two stack.
    <div className="mt-2 rounded-lg overflow-hidden border border-warning/30 bg-warning/10">
      {isMulti ? (
        <>
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            aria-expanded={expanded}
            className="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-warning/15 aria-expanded:bg-warning/15 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          >
            <AlarmClock className="h-3.5 w-3.5 shrink-0 text-warning" />
            <span className="flex min-w-0 items-center gap-1.5 text-xs">
              <span className="truncate font-medium text-foreground">
                {wakeups.length} planlagte kørsler
              </span>
              <span className="text-muted-foreground truncate">
                næste: {wakeupLabel(soonest)}
              </span>
              <span className="text-warning tabular-nums shrink-0">
                {wakeupCounter(soonest, now)}
              </span>
            </span>
            <ChevronDown
              className={`ml-auto h-3.5 w-3.5 text-muted-foreground shrink-0 transition-transform ${expanded ? "" : "-rotate-90"}`}
            />
          </button>
          {expanded && (
            <div className="divide-y divide-warning/20 border-t border-warning/20">
              {wakeups.map((w) => (
                <WakeupBarRow
                  key={w.id}
                  wakeup={w}
                  now={now}
                  onCancel={() => handleCancel(w.id)}
                  cancelling={cancellingIds.has(w.id)}
                />
              ))}
            </div>
          )}
        </>
      ) : (
        <WakeupBarRow
          wakeup={soonest}
          now={now}
          onCancel={() => handleCancel(soonest.id)}
          cancelling={cancellingIds.has(soonest.id)}
        />
      )}
    </div>
  );
}
