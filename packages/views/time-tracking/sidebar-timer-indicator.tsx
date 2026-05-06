"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { Square, X, Clock } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useTimerStore } from "@multica/core/time-entries/timer-store";
import { useCreateTimeEntry } from "@multica/core/time-entries/mutations";
import { redmineActivitiesOptions } from "@multica/core/time-entries/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useQuery } from "@tanstack/react-query";
import { useNavigation } from "../navigation";
import { useCurrentWorkspace } from "@multica/core/paths";
import { toast } from "sonner";
import { useT } from "../i18n";

function formatElapsed(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${pad(h)}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
}

function formatDurationShort(minutes: number): string {
  if (minutes < 60) return `${minutes}m`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

export function SidebarTimerIndicator() {
  const { t } = useT("time-tracking");
  const timer = useTimerStore((s) => s.activeTimer);
  const stopTimer = useTimerStore((s) => s.stopTimer);
  const discardTimer = useTimerStore((s) => s.discardTimer);
  const setActivity = useTimerStore((s) => s.setActivity);

  const [open, setOpen] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [comment, setComment] = useState("");
  const commentRef = useRef<HTMLInputElement>(null);

  const wsId = useWorkspaceId();
  const createEntry = useCreateTimeEntry();
  const { data: activitiesData } = useQuery({
    ...redmineActivitiesOptions(wsId),
    enabled: !!wsId && !!timer,
  });
  const activities = activitiesData?.activities ?? [];

  const navigation = useNavigation();
  const workspace = useCurrentWorkspace();

  useEffect(() => {
    if (!timer) {
      setElapsed(0);
      return;
    }
    const update = () => setElapsed(Date.now() - timer.startedAt);
    update();
    const interval = setInterval(update, 1000);
    return () => clearInterval(interval);
  }, [timer]);

  useEffect(() => {
    if (open) {
      setTimeout(() => commentRef.current?.focus(), 100);
    }
  }, [open]);

  const handleStop = useCallback(() => {
    const result = stopTimer();
    if (!result) return;

    const activityId = timer?.activityId;
    const activityName = timer?.activityName;

    createEntry.mutate(
      {
        issueId: result.issueId,
        data: {
          duration_minutes: result.durationMinutes,
          redmine_activity_id: activityId,
          activity_name: activityName,
          comment: comment || undefined,
          spent_on: new Date().toISOString().split("T")[0],
          timer_started_at: result.startedAt,
          timer_stopped_at: result.stoppedAt,
        },
      },
      {
        onSuccess: (entry) => {
          const syncLabel =
            entry.sync_status === "synced" ? " → synced to Redmine" : "";
          toast.success(
            `Logged ${formatDurationShort(result.durationMinutes)}${syncLabel}`,
          );
        },
        onError: () => {
          toast.error("Failed to log time entry");
        },
      },
    );

    setComment("");
    setOpen(false);
  }, [stopTimer, timer, comment, createEntry]);

  const handleDiscard = useCallback(() => {
    discardTimer();
    setComment("");
    setOpen(false);
  }, [discardTimer]);

  const handleNavigateToIssue = useCallback(() => {
    if (!timer || !workspace) return;
    navigation.push(`/${workspace.slug}/issues/${timer.issueId}`);
    setOpen(false);
  }, [timer, workspace, navigation]);

  if (!timer) return null;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-accent/70">
        {/* Pulsing dot */}
        <span className="relative flex size-2 shrink-0">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-red-400 opacity-75" />
          <span className="relative inline-flex size-2 rounded-full bg-red-500" />
        </span>
        <span className="font-mono tabular-nums text-muted-foreground">
          {formatElapsed(elapsed)}
        </span>
        <span className="truncate font-medium text-foreground">
          {timer.issueIdentifier}
        </span>
      </PopoverTrigger>

      <PopoverContent side="top" align="start" sideOffset={8}>
        {/* Issue title */}
        <button
          className="flex w-full items-center gap-2 text-left text-xs text-muted-foreground transition-colors hover:text-foreground"
          onClick={handleNavigateToIssue}
        >
          <Clock className="size-3 shrink-0" />
          <span className="truncate">{timer.issueTitle}</span>
        </button>

        {/* Activity selector */}
        {activities.length > 0 && (
          <select
            className="w-full rounded-md border bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
            value={timer.activityId ?? ""}
            onChange={(e) => {
              const id = Number(e.target.value);
              const act = activities.find((a) => a.id === id);
              if (act) setActivity(act.id, act.name);
            }}
          >
            <option value="">Activity type...</option>
            {activities.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        )}

        {/* Comment input */}
        <input
          ref={commentRef}
          type="text"
          placeholder="What did you work on?"
          className="w-full rounded-md border bg-background px-2 py-1.5 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") handleStop();
          }}
        />

        {/* Action buttons */}
        <div className="flex gap-2">
          <Button
            variant="ghost"
            size="xs"
            className="flex-1 text-destructive hover:text-destructive"
            onClick={handleDiscard}
          >
            <X className="mr-1 size-3" />
            {t($ => $.timer_discard)}
          </Button>
          <Button
            size="xs"
            className="flex-1"
            onClick={handleStop}
            disabled={createEntry.isPending}
          >
            <Square className="mr-1 size-3" />
            {t($ => $.timer_stop_log)}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
