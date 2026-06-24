"use client";

// FIR-1931: the context bar is clickable and opens this sheet, which breaks the
// issue's spend down PER SESSION (= per thread) — cost, input/output tokens,
// cache, and the session's current context fullness — with an issue total on
// top. Built from data we already have (task_usage + the last-turn footprint).

import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@multica/ui/components/ui/sheet";
import { SessionTimelineChart } from "./session-timeline-chart";
import { useUsageBreakdown } from "./use-sessions";
import type { SessionUsageRow } from "./types";

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return `${n}`;
}

function formatCost(cents: number): string {
  const dollars = cents / 100;
  // Enough precision for sub-cent sessions without trailing noise.
  const digits = dollars > 0 && dollars < 0.01 ? 4 : 2;
  return `$${dollars.toFixed(digits)}`;
}

function sessionTitle(row: SessionUsageRow, index: number): string {
  const t = row.title.trim();
  if (t) return t;
  return `Session ${index + 1}`;
}

export function SessionUsageSheet({
  issueId,
  open,
  onOpenChange,
}: {
  issueId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  // Only fetch while the sheet is open.
  const { data, isLoading } = useUsageBreakdown(issueId, open);
  const sessions = data?.sessions ?? [];

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex flex-col gap-0 p-0 sm:max-w-md">
        <SheetHeader className="shrink-0 border-b px-5 py-4">
          <SheetTitle className="text-sm font-semibold">Context &amp; cost by session</SheetTitle>
          <SheetDescription className="text-xs text-muted-foreground">
            Each session is one thread. Cost, tokens and cache are summed per
            session; the total is the whole issue.
          </SheetDescription>
        </SheetHeader>

        {/* Issue total */}
        <div className="shrink-0 grid grid-cols-3 gap-x-4 border-b bg-muted/20 px-5 py-3 text-xs">
          <div className="flex flex-col gap-0.5">
            <span className="text-muted-foreground">Total cost</span>
            <span className="font-medium tabular-nums">
              {isLoading ? "…" : formatCost(data?.total_cost_cents ?? 0)}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-muted-foreground">Input</span>
            <span className="font-medium tabular-nums">
              {isLoading ? "…" : formatTokens(data?.total_input_tokens ?? 0)}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-muted-foreground">Output</span>
            <span className="font-medium tabular-nums">
              {isLoading ? "…" : formatTokens(data?.total_output_tokens ?? 0)}
            </span>
          </div>
        </div>

        {/* Per-session list — scrollable */}
        <div className="flex-1 min-h-0 overflow-y-auto">
          {isLoading ? (
            <div className="space-y-2 p-5">
              {[0, 1, 2].map((i) => (
                <div key={i} className="h-20 animate-pulse rounded-lg bg-muted" />
              ))}
            </div>
          ) : sessions.length === 0 ? (
            <div className="p-5">
              <div className="rounded-lg border bg-muted/30 px-4 py-6 text-center text-xs text-muted-foreground">
                No agent runs on this issue yet.
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-3 p-5">
              {sessions.map((s, i) => (
                <div key={s.session_id} className="rounded-lg border px-4 py-3">
                  <div className="mb-2 flex items-start justify-between gap-2">
                    <span className="min-w-0 truncate text-xs font-medium text-foreground">
                      {sessionTitle(s, i)}
                    </span>
                    <span className="shrink-0 text-xs font-semibold tabular-nums">
                      {formatCost(s.cost_cents)}
                    </span>
                  </div>
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
                    <span>
                      Input <span className="tabular-nums text-foreground">{formatTokens(s.input_tokens)}</span>
                    </span>
                    <span>
                      Output <span className="tabular-nums text-foreground">{formatTokens(s.output_tokens)}</span>
                    </span>
                    <span>
                      Cache read <span className="tabular-nums text-foreground">{formatTokens(s.cache_read_tokens)}</span>
                    </span>
                    <span>
                      Cache write <span className="tabular-nums text-foreground">{formatTokens(s.cache_write_tokens)}</span>
                    </span>
                    <span>
                      Runs <span className="tabular-nums text-foreground">{s.run_count}</span>
                    </span>
                    {s.max_context_tokens > 0 ? (
                      <span>
                        Context{" "}
                        <span className="tabular-nums text-foreground">
                          {s.approximate ? "~" : ""}
                          {s.used_percent}%
                        </span>
                      </span>
                    ) : null}
                  </div>
                  {/* Development curve over the session (one point per run). */}
                  <div className="mt-3">
                    <p className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                      Context over the session
                    </p>
                    <SessionTimelineChart
                      issueId={issueId}
                      sessionId={s.session_id}
                      enabled={open}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
