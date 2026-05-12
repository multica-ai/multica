"use client";

import { useState } from "react";
import { Activity, ChevronRight, Circle, GitPullRequest } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { CRAttempt, CRSignal } from "@multica/core/types";
import { crAttemptListOptions, crSignalListOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { timeAgo } from "@multica/core/utils";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";

const outcomeTone: Record<string, string> = {
  completed_clean: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  completed_with_findings: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  silent_partial: "border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300",
  silent_total: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
  failed: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
  skipped: "border-slate-500/30 bg-slate-500/10 text-slate-700 dark:text-slate-300",
};

function label(value: string | null | undefined) {
  return value ? value.replaceAll("_", " ") : "pending";
}

function formatSignalSummary(signal: CRSignal) {
  const summary = signal.payload_summary ?? {};
  const parts = [
    typeof summary.name === "string" ? summary.name : null,
    typeof summary.state === "string" ? summary.state : null,
    typeof summary.conclusion === "string" ? summary.conclusion : null,
    typeof summary.path === "string" ? summary.path : null,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : label(signal.signal_action);
}

function SignalList({ issueId, attemptId }: { issueId: string; attemptId: string }) {
  const wsId = useWorkspaceId();
  const { data: signals = [], isLoading } = useQuery(crSignalListOptions(wsId, issueId, attemptId));
  if (isLoading) {
    return <div className="px-2 py-1 text-[11px] text-muted-foreground">Loading signals...</div>;
  }
  if (signals.length === 0) {
    return <div className="px-2 py-1 text-[11px] text-muted-foreground">No signals recorded.</div>;
  }
  return (
    <div className="space-y-1 px-2 pb-2">
      {signals.map((signal) => (
        <div key={signal.id} className="grid grid-cols-[14px_1fr] gap-1.5 text-[11px]">
          <Circle className="mt-1 h-2 w-2 fill-muted-foreground/40 text-muted-foreground/40" />
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-1.5">
              <span className="shrink-0 font-medium">{label(signal.signal_kind)}</span>
              <span className="truncate text-muted-foreground">{formatSignalSummary(signal)}</span>
            </div>
            <div className="text-muted-foreground">{timeAgo(signal.received_at)}</div>
          </div>
        </div>
      ))}
    </div>
  );
}

function AttemptRow({ issueId, attempt }: { issueId: string; attempt: CRAttempt }) {
  const [open, setOpen] = useState(false);
  const outcome = attempt.outcome ?? "pending";
  return (
    <div className="rounded-md border border-border/70">
      <button
        type="button"
        className="flex w-full items-start gap-2 px-2 py-2 text-left"
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronRight className={cn("mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="shrink-0 text-xs font-medium">Round {attempt.cr_round}</span>
            <Badge variant="outline" className={cn("h-5 truncate px-1.5 text-[10px] capitalize", outcomeTone[outcome])}>
              {label(outcome)}
            </Badge>
          </div>
          <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            <Activity className="h-3 w-3 shrink-0" />
            <span className="truncate">{label(attempt.first_signal_kind)} first</span>
            <span className="shrink-0">·</span>
            <span className="shrink-0">{attempt.findings_count} findings</span>
          </div>
        </div>
      </button>
      {open && <SignalList issueId={issueId} attemptId={attempt.id} />}
    </div>
  );
}

export function CRActivityPanel({ issueId, issueStatus }: { issueId: string; issueStatus?: string }) {
  const wsId = useWorkspaceId();
  const { data: attempts = [], isLoading } = useQuery(
    crAttemptListOptions(wsId, issueId, issueStatus === "coderabbit"),
  );

  if (isLoading) {
    return (
      <div className="space-y-2 pl-2">
        <div className="h-8 rounded-md bg-muted/50" />
        <div className="h-8 rounded-md bg-muted/50" />
      </div>
    );
  }
  if (attempts.length === 0) {
    return null;
  }

  return (
    <div className="space-y-2 pl-2">
      {attempts.map((attempt) => (
        <AttemptRow key={attempt.id} issueId={issueId} attempt={attempt} />
      ))}
      {attempts[0]?.pr_url && (
        <a
          href={attempts[0].pr_url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <GitPullRequest className="h-3.5 w-3.5" />
          Latest PR
        </a>
      )}
    </div>
  );
}
