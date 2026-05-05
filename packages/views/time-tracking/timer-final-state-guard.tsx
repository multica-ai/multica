"use client";

import { useCallback, useRef } from "react";
import { useTimerStore } from "@multica/core/time-entries/timer-store";
import { useCreateTimeEntry } from "@multica/core/time-entries/mutations";
import { useWSEvent } from "@multica/core/realtime";
import type { IssueUpdatedPayload } from "@multica/core/types";
import { toast } from "sonner";

const FINAL_ISSUE_STATUSES = ["done", "cancelled"] as const;

function formatDurationShort(minutes: number): string {
  if (minutes < 60) return `${minutes}m`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

/**
 * Headless guard component. Subscribes to issue:updated WS events and
 * automatically stops + logs the active timer when its issue moves to a
 * final state (done / cancelled).
 *
 * Follows the same pattern as IdleDetector — no UI, mounted once at the
 * layout level so it is always active regardless of which page is open.
 */
export function TimerFinalStateGuard() {
  const stopTimer = useTimerStore((s) => s.stopTimer);
  const createEntry = useCreateTimeEntry();

  // Keep a ref to avoid the WS handler capturing a stale activeTimer value.
  const timerRef = useRef(useTimerStore.getState().activeTimer);
  useTimerStore.subscribe((state) => {
    timerRef.current = state.activeTimer;
  });

  const handleIssueUpdated = useCallback(
    (payload: unknown) => {
      const { issue } = payload as IssueUpdatedPayload;
      if (!issue?.id) return;
      if (
        !FINAL_ISSUE_STATUSES.includes(
          issue.status as (typeof FINAL_ISSUE_STATUSES)[number],
        )
      )
        return;

      const activeTimer = timerRef.current;
      if (!activeTimer || activeTimer.issueId !== issue.id) return;

      const result = stopTimer();
      if (!result) return;

      createEntry.mutate(
        {
          issueId: result.issueId,
          data: {
            duration_minutes: result.durationMinutes,
            redmine_activity_id: activeTimer.activityId,
            activity_name: activeTimer.activityName,
            spent_on: new Date().toISOString().split("T")[0],
            timer_started_at: result.startedAt,
            timer_stopped_at: result.stoppedAt,
          },
        },
        {
          onSuccess: (entry) => {
            const syncLabel =
              entry.sync_status === "synced" ? " → synced to Redmine" : "";
            toast.info(
              `Timer stopped: issue moved to ${issue.status}. Logged ${formatDurationShort(result.durationMinutes)}${syncLabel}`,
            );
          },
          onError: () => {
            toast.error("Timer stopped but failed to log time entry");
          },
        },
      );
    },
    [stopTimer, createEntry],
  );

  useWSEvent("issue:updated", handleIssueUpdated);

  return null;
}
