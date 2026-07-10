"use client";

// FIR-1787 (review of FIR-1769 P3) — the context-window indicator is no longer
// a whisper-thin hairline tucked under the active session header. Jesper's
// review: it must live as the line directly above the comment input, fill with
// colour as context is consumed, state how much is used and how much is left,
// warm to orange past 50%, and past 60% recommend a handoff with a button.
//
// Fed by the truthful cross-runtime/model measurement (FIR-1709) when a run
// with usage exists in the active session; before that it falls back to a
// labelled content-size estimate so the bar is never blank or misleadingly
// exact.

import { useEffect, useState, type ReactNode } from "react";
import { ArrowRightLeft } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { estimateSessionContextFraction } from "./context-estimate";
import { SessionUsageSheet } from "./session-usage-sheet";
import { useContextUsage, useStartFresh } from "./use-sessions";
import type { TimelineGroup } from "./grouping";

// Jesper's thresholds (FIR-1787): orange from 50%, recommend handoff from 60%.
const WARM_THRESHOLD = 0.5;
const HANDOFF_THRESHOLD = 0.6;

// FIR-2279: Anthropic keeps the prompt cache warm for ~5 minutes after the last
// call (the API default TTL; our pricing table assumes the same). Past that the
// cache goes cold and the next run re-pays the full cache-write on the whole
// context instead of a cheap read — so the bar counts down from the last run to
// this line and then flips to a "cold" state.
const CACHE_TTL_MS = 5 * 60 * 1000;
// Amber the timer in its final minute — the last moment resuming the session is
// still cheap.
const CACHE_URGENT_MS = 60 * 1000;

function formatCountdown(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

function formatTokens(n: number): string {
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return `${n}`;
}

export function SessionContextBar({
  issueId,
  sessionId,
  groups,
  active = true,
}: {
  issueId: string;
  // FIR-1870: scope the bar to one session so each session shows its own context
  // window; omit for the active session.
  sessionId?: string;
  groups: TimelineGroup[];
  // Only the active session offers the handoff CTA; historical sessions show the
  // fill bar as a read-only record of how full they got.
  active?: boolean;
}) {
  const { data } = useContextUsage(issueId, sessionId);
  const startFresh = useStartFresh(issueId);
  // FIR-1931: clicking the bar opens the per-session cost/token/cache sheet.
  const [sheetOpen, setSheetOpen] = useState(false);

  // FIR-2279: prompt-cache warm/cold countdown. Only the active session can be
  // resumed cheaply, so only it shows the timer; a historical session's cache is
  // always long cold. lastActivity anchors the 5-minute window.
  const lastActivity = active && data?.has_data ? data.last_activity_at ?? null : null;
  const [nowMs, setNowMs] = useState(() => Date.now());
  useEffect(() => {
    if (!lastActivity) return;
    const expiresAt = new Date(lastActivity).getTime() + CACHE_TTL_MS;
    setNowMs(Date.now());
    // Nothing left to count down once the cache is cold — stop ticking and stay
    // on the cold state rather than spinning a 1s interval forever.
    if (Date.now() >= expiresAt) return;
    const id = setInterval(() => {
      const t = Date.now();
      setNowMs(t);
      if (t >= expiresAt) clearInterval(id);
    }, 1000);
    return () => clearInterval(id);
  }, [lastActivity]);

  let cacheTimer: ReactNode = null;
  if (lastActivity) {
    const remainingMs = new Date(lastActivity).getTime() + CACHE_TTL_MS - nowMs;
    if (remainingMs > 0) {
      const urgent = remainingMs <= CACHE_URGENT_MS;
      cacheTimer = (
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <span
            className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${
              urgent ? "bg-amber-500" : "bg-muted-foreground/40"
            }`}
          />
          <span className={urgent ? "text-amber-600 dark:text-amber-400" : undefined}>
            Cache warm · {formatCountdown(remainingMs)} left
          </span>
        </div>
      );
    } else {
      cacheTimer = (
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground/40" />
          <span>Cache cold · next run re-pays the full context</span>
        </div>
      );
    }
  }

  let fraction: number;
  let approximate: boolean;
  let detail = "";
  if (data?.has_data && data.max_context_tokens > 0) {
    fraction = data.used_percent / 100;
    // FIR-1931 Fix C: a run with no last-turn footprint falls back to the
    // cumulative sum, which the server clamps to the window and flags as
    // approximate. Carry that through so the bar prefixes "~" instead of
    // implying the over-counted figure is exact.
    approximate = data.approximate === true;
    const cache =
      data.cache_share_percent > 0 ? ` · ${data.cache_share_percent}% from cache` : "";
    const model = data.model ? ` · ${data.model}` : "";
    // FIR-1960: surface compaction tracking in the bar itself. A compaction is a
    // sharp context drop after a fairly-full run — showing the count explains a
    // sudden fall in fullness instead of it looking like a mystery jump.
    const compactions = data.compactions ?? 0;
    const compaction =
      compactions > 0
        ? ` · ${compactions} compaction${compactions === 1 ? "" : "s"}`
        : "";
    // Show the whole prompt the model read, not just fresh input. Fall back to
    // deriving it if an older server hasn't sent context_tokens yet.
    const ctxTokens =
      data.context_tokens ||
      data.input_tokens + data.cache_read_tokens + data.cache_write_tokens;
    detail = `${formatTokens(ctxTokens)} / ${formatTokens(
      data.max_context_tokens,
    )} tokens${cache}${compaction}${model}`;
  } else {
    fraction = estimateSessionContextFraction(groups);
    approximate = true;
  }

  const pct = Math.max(0, Math.min(1, fraction));
  const usedPct = Math.round(pct * 100);
  const leftPct = 100 - usedPct;
  // FIR-1960: an approximate (estimated) reading must NEVER drive the loud red
  // "hand off" state — that was the false `~100% context used` lock-up Jesper
  // flagged on runs with no last-turn footprint. Only a real measured footprint
  // may warm the bar or recommend a handoff; an estimate stays neutral and is
  // honestly labelled "estimate", so we say "we're not sure" instead of "full".
  const warm = !approximate && pct >= WARM_THRESHOLD;
  const recommendHandoff = active && !approximate && pct >= HANDOFF_THRESHOLD;

  // Quiet until half full, then warm to orange, then to destructive near the
  // handoff line — semantic/amber tokens only, no custom colours.
  const fill = recommendHandoff
    ? "bg-destructive"
    : warm
      ? "bg-amber-500"
      : "bg-muted-foreground/40";
  const usedText = recommendHandoff
    ? "text-destructive"
    : warm
      ? "text-amber-600 dark:text-amber-400"
      : "text-muted-foreground";

  async function handoff() {
    // FIR-1874: hand off THIS session = resolve its thread root and store a
    // handoff. sessionId is the thread root_comment_id in the thread=session model.
    if (sessionId) await startFresh.mutateAsync({ root_comment_id: sessionId });
  }

  return (
    <div className="mb-2 space-y-1.5">
      {/* FIR-1931: the label + fill row is a button that opens the per-session
          cost/token/cache sheet. The Handoff button stays a separate sibling
          below, so the two clickable affordances never nest. */}
      <button
        type="button"
        onClick={() => setSheetOpen(true)}
        aria-label="Show context and cost by session"
        className="block w-full space-y-1.5 text-left rounded-sm transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      >
        <div className="flex items-center justify-between gap-2 text-[11px]">
          <span className={`font-medium ${usedText}`}>
            {approximate ? "~" : ""}
            {usedPct}% context used
          </span>
          <span className="text-muted-foreground">
            {leftPct}% left
            {detail ? ` · ${detail}` : ""}
            {approximate ? " · estimate" : ""}
          </span>
        </div>
        <div className="h-1 w-full overflow-hidden rounded-full bg-border">
          <div className={`h-full ${fill} transition-all`} style={{ width: `${pct * 100}%` }} />
        </div>
      </button>
      {cacheTimer}
      <SessionUsageSheet issueId={issueId} open={sheetOpen} onOpenChange={setSheetOpen} />
      {recommendHandoff ? (
        <div className="flex items-center justify-between gap-2 pt-0.5">
          <span className="text-[11px] text-muted-foreground">
            This session is filling up — hand off to a fresh session to keep the agent sharp.
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 shrink-0 gap-1.5 text-xs"
            onClick={handoff}
            disabled={startFresh.isPending}
          >
            <ArrowRightLeft className="h-3.5 w-3.5" />
            Handoff
          </Button>
        </div>
      ) : null}
    </div>
  );
}
