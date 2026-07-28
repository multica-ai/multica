"use client";

// FIR-3901 — the red bar at the top of an issue whose last run failed and that
// nothing will pick up again. Collapsed it answers "something broke here";
// expanded it answers "why" (the existing RunFailureCard: reason + last step +
// output tail) and offers the two ways forward.
//
// Deliberately a sibling of AgentLiveCard rather than a state inside it: that
// card reconciles against the *active* task set and drops anything not in it,
// which is what self-heals a stale "is working" banner. A failed run is by
// definition not active, so it needs its own state.

import { useState } from "react";
import { AlertCircle, ChevronDown, Loader2, Play, RotateCcw } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { resolveFailureReasonLabel } from "../failure-reason-label";
import { RunFailureCard } from "./run-failure-card";
import {
  ISSUE_FAILED_RUNS_KEY,
  useIssueFailedRuns,
  type DeadFailedRun,
} from "../dead-failed-runs";

interface TimelineItem {
  seq: number;
  type: string;
  tool?: string;
  input?: Record<string, unknown>;
  output?: string;
  content?: string;
}

/**
 * Maps raw task messages onto the shape RunFailureCard reads. Defensive on
 * purpose: a message shape the server adds later must not break the card.
 */
export function toFailureTimeline(messages: unknown): TimelineItem[] {
  if (!Array.isArray(messages)) return [];
  return messages.map((m, i) => {
    const row = (m ?? {}) as Record<string, unknown>;
    return {
      seq: typeof row.seq === "number" ? row.seq : i,
      type: typeof row.type === "string" ? row.type : "",
      tool: typeof row.tool === "string" ? row.tool : undefined,
      input:
        row.input && typeof row.input === "object"
          ? (row.input as Record<string, unknown>)
          : undefined,
      output: typeof row.output === "string" ? row.output : undefined,
      content: typeof row.content === "string" ? row.content : undefined,
    };
  });
}

export function FailedRunBar({
  issueId,
  enabled = true,
}: {
  issueId: string;
  enabled?: boolean;
}) {
  const runs = useIssueFailedRuns(issueId, enabled);
  const run = runs[0];
  if (!run) return null;
  return <FailedRunBarInner key={run.task_id} issueId={issueId} run={run} />;
}

function FailedRunBarInner({ issueId, run }: { issueId: string; run: DeadFailedRun }) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<"resume" | "fresh" | null>(null);
  const qc = useQueryClient();

  // The transcript is only needed once the user opens the bar — a failed-run
  // banner must not pull a full transcript for every issue you glance at.
  const { data: messages } = useQuery({
    queryKey: ["cerebro-failed-run-messages", run.task_id],
    queryFn: () => api.listTaskMessages(run.task_id),
    enabled: open,
    staleTime: 60_000,
  });

  const label = resolveFailureReasonLabel(run.failure_reason) ?? "Run failed";

  const start = async (mode: "resume" | "fresh") => {
    if (busy) return;
    setBusy(mode);
    try {
      await api.rerunIssue(issueId, run.task_id, mode === "resume");
      toast.success(mode === "resume" ? "Continuing where it stopped" : "Started over");
      // The new task makes this failure no longer dead, so the bar clears on
      // the next read.
      await qc.invalidateQueries({ queryKey: ISSUE_FAILED_RUNS_KEY(issueId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not start the run");
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="mt-4 sticky top-4 z-10 rounded-lg bg-background/80 supports-[backdrop-filter]:bg-background/55 backdrop-blur-md">
      <div className="overflow-hidden rounded-lg border border-destructive/30 bg-destructive/10">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          data-testid="failed-run-bar"
          className="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-destructive/15 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
        >
          <AlertCircle aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-destructive" />
          <span className="truncate text-xs font-medium text-destructive">{label}</span>
          {run.runtime_name ? (
            <span className="hidden truncate text-xs text-muted-foreground sm:inline">
              {run.runtime_name}
            </span>
          ) : null}
          <ChevronDown
            className={cn(
              "ml-auto h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform",
              open ? "" : "-rotate-90",
            )}
          />
        </button>

        {open ? (
          <div className="border-t border-destructive/20">
            <div className="pt-3">
              <RunFailureCard failureReason={run.failure_reason} items={toFailureTimeline(messages)} />
            </div>
            <div className="flex flex-wrap items-center gap-2 px-4 pb-3">
              <button
                type="button"
                disabled={!run.resume_possible || busy !== null}
                onClick={() => void start("resume")}
                title={run.resume_possible ? "Continue the same conversation" : run.blocked_reason}
                className="inline-flex items-center gap-1.5 rounded border border-border bg-background px-2.5 py-1 text-xs font-medium transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busy === "resume" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Play className="h-3 w-3" />
                )}
                Resume
              </button>
              <button
                type="button"
                disabled={busy !== null}
                onClick={() => void start("fresh")}
                title="Start over with a blank conversation"
                className="inline-flex items-center gap-1.5 rounded border border-border bg-background px-2.5 py-1 text-xs font-medium transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busy === "fresh" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <RotateCcw className="h-3 w-3" />
                )}
                Start over
              </button>
              {!run.resume_possible && run.blocked_reason ? (
                <span className="text-xs text-muted-foreground">{run.blocked_reason}</span>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
