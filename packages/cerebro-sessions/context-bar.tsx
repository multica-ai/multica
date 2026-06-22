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

import { ArrowRightLeft } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { estimateSessionContextFraction } from "./context-estimate";
import { useContextUsage, useStartFresh } from "./use-sessions";
import type { TimelineGroup } from "./grouping";

// Jesper's thresholds (FIR-1787): orange from 50%, recommend handoff from 60%.
const WARM_THRESHOLD = 0.5;
const HANDOFF_THRESHOLD = 0.6;

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

  let fraction: number;
  let approximate: boolean;
  let detail = "";
  if (data?.has_data && data.max_context_tokens > 0) {
    fraction = data.used_percent / 100;
    approximate = false;
    const cache =
      data.cache_share_percent > 0 ? ` · ${data.cache_share_percent}% from cache` : "";
    const model = data.model ? ` · ${data.model}` : "";
    // Show the whole prompt the model read, not just fresh input. Fall back to
    // deriving it if an older server hasn't sent context_tokens yet.
    const ctxTokens =
      data.context_tokens ||
      data.input_tokens + data.cache_read_tokens + data.cache_write_tokens;
    detail = `${formatTokens(ctxTokens)} / ${formatTokens(
      data.max_context_tokens,
    )} tokens${cache}${model}`;
  } else {
    fraction = estimateSessionContextFraction(groups);
    approximate = true;
  }

  const pct = Math.max(0, Math.min(1, fraction));
  const usedPct = Math.round(pct * 100);
  const leftPct = 100 - usedPct;
  const warm = pct >= WARM_THRESHOLD;
  const recommendHandoff = active && pct >= HANDOFF_THRESHOLD;

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
      <div className="flex items-center justify-between gap-2 text-[11px]">
        <span className={`font-medium ${usedText}`}>
          {approximate ? "~" : ""}
          {usedPct}% context used
        </span>
        <span className="text-muted-foreground">
          {leftPct}% left
          {detail ? ` · ${detail}` : approximate ? " · estimate" : ""}
        </span>
      </div>
      <div className="h-1 w-full overflow-hidden rounded-full bg-border">
        <div className={`h-full ${fill} transition-all`} style={{ width: `${pct * 100}%` }} />
      </div>
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
