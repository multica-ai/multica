"use client";

import { useEffect, useMemo, useState } from "react";
import { Clock, Play, Square } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { issueTimerOptions } from "@multica/core/issues/queries";
import {
  useStartIssueTimer,
  useStopIssueTimer,
} from "@multica/core/issues/mutations";

function formatDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
  }
  return `${minutes}:${String(secs).padStart(2, "0")}`;
}

export function IssueTimerSection({ issueId }: { issueId: string }) {
  const [now, setNow] = useState(() => Date.now());
  const { data: timer, dataUpdatedAt } = useQuery(issueTimerOptions(issueId));
  const startTimer = useStartIssueTimer(issueId);
  const stopTimer = useStopIssueTimer(issueId);

  useEffect(() => {
    if (!timer?.active_timer) return;
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, [timer?.active_timer?.id]);

  const displayedSeconds = useMemo(() => {
    const base = timer?.total_seconds ?? 0;
    if (!timer?.active_timer) return base;
    return base + Math.max(0, Math.floor((now - dataUpdatedAt) / 1000));
  }, [timer?.total_seconds, timer?.active_timer, dataUpdatedAt, now]);

  const isRunning = !!timer?.active_timer;
  const isPending = startTimer.isPending || stopTimer.isPending;

  return (
    <div className="flex items-center gap-2 rounded-md border border-border/60 bg-muted/25 px-2 py-2">
      <Clock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="font-mono text-sm tabular-nums">
          {formatDuration(displayedSeconds)}
        </div>
        <div className="truncate text-[11px] text-muted-foreground">
          {isRunning ? "Timer running" : "Tracked time"}
        </div>
      </div>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              type="button"
              variant={isRunning ? "secondary" : "outline"}
              size="icon-sm"
              disabled={isPending}
              onClick={() => {
                const mutation = isRunning ? stopTimer : startTimer;
                mutation.mutate(undefined, {
                  onError: (err) => {
                    const message =
                      err instanceof Error ? err.message : "Timer update failed";
                    toast.error(message);
                  },
                });
              }}
            >
              {isRunning ? <Square /> : <Play />}
            </Button>
          }
        />
        <TooltipContent>{isRunning ? "Stop timer" : "Start timer"}</TooltipContent>
      </Tooltip>
    </div>
  );
}
