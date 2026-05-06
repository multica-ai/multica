"use client";

import { useEffect, useState, useCallback } from "react";
import { useTimerStore } from "@multica/core/time-entries/timer-store";
import {
  useIdleStore,
  startIdleTracking,
} from "@multica/core/time-entries/idle-store";
import { useCreateTimeEntry } from "@multica/core/time-entries/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { useT } from "../i18n";

function formatDuration(ms: number): string {
  const mins = Math.round(ms / 60000);
  if (mins < 60) return `${mins}m`;
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

/**
 * Shows a dialog when the user has been idle for 15 minutes while a timer
 * is running. Offers to: keep running, adjust (subtract idle time), or stop & log.
 */
export function IdleDetector() {
  const { t } = useT("time-tracking");
  const timer = useTimerStore((s) => s.activeTimer);
  const stopTimer = useTimerStore((s) => s.stopTimer);
  const isIdle = useIdleStore((s) => s.isIdle);
  const idleSince = useIdleStore((s) => s.idleSince);
  const dismissIdle = useIdleStore((s) => s.dismissIdle);
  const createEntry = useCreateTimeEntry();
  const [visible, setVisible] = useState(false);

  // Start tracking when component mounts
  useEffect(() => {
    startIdleTracking();
  }, []);

  // Show dialog when timer is running + user is idle
  useEffect(() => {
    if (timer && isIdle) {
      setVisible(true);
    } else {
      setVisible(false);
    }
  }, [timer, isIdle]);

  const handleKeepRunning = useCallback(() => {
    dismissIdle();
    setVisible(false);
  }, [dismissIdle]);

  const handleAdjust = useCallback(() => {
    if (!timer || !idleSince) return;
    // Stop timer but adjust: the "real" work ended at idleSince
    const activeMs = idleSince - timer.startedAt;
    const durationMinutes = Math.max(1, Math.round(activeMs / 60000));

    const startedAt = new Date(timer.startedAt).toISOString();
    const stoppedAt = new Date(idleSince).toISOString();

    // Clear the timer store
    useTimerStore.getState().discardTimer();
    dismissIdle();
    setVisible(false);

    createEntry.mutate(
      {
        issueId: timer.issueId,
        data: {
          duration_minutes: durationMinutes,
          redmine_activity_id: timer.activityId,
          activity_name: timer.activityName,
          spent_on: new Date().toISOString().split("T")[0],
          timer_started_at: startedAt,
          timer_stopped_at: stoppedAt,
        },
      },
      {
        onSuccess: (entry) => {
          const syncLabel =
            entry.sync_status === "synced" ? " → synced to Redmine" : "";
          toast.success(
            `Logged ${formatDuration(activeMs)} (idle time excluded)${syncLabel}`,
          );
        },
      },
    );
  }, [timer, idleSince, dismissIdle, createEntry]);

  const handleStopAndLog = useCallback(() => {
    const result = stopTimer();
    if (!result) return;
    dismissIdle();
    setVisible(false);

    createEntry.mutate(
      {
        issueId: result.issueId,
        data: {
          duration_minutes: result.durationMinutes,
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
            `Logged ${formatDuration(result.durationMinutes * 60000)} (including idle)${syncLabel}`,
          );
        },
      },
    );
  }, [stopTimer, dismissIdle, createEntry]);

  if (!visible || !timer || !idleSince) return null;

  const idleDuration = Date.now() - idleSince;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 animate-in fade-in-0 duration-200">
      <div className="mx-4 w-full max-w-sm rounded-xl border bg-popover p-5 shadow-2xl animate-in zoom-in-95 slide-in-from-bottom-2 duration-200">
        <div className="mb-1 text-sm font-medium">{t($ => $.idle_title)}</div>
        <p className="mb-4 text-xs text-muted-foreground">
          {t($ => $.idle_body, {
            duration: formatDuration(idleDuration),
            issue: timer.issueIdentifier,
          })}
        </p>
        <div className="flex flex-col gap-2">
          <Button size="sm" variant="default" onClick={handleKeepRunning}>
            {t($ => $.idle_keep_running)}
          </Button>
          <Button size="sm" variant="outline" onClick={handleAdjust}>
            {t($ => $.idle_subtract_idle, { duration: formatDuration(idleDuration) })}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="text-muted-foreground"
            onClick={handleStopAndLog}
          >
            {t($ => $.idle_stop_all)}
          </Button>
        </div>
      </div>
    </div>
  );
}
