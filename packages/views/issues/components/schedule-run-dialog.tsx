"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Issue } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueScheduleOptions } from "@multica/core/issues/queries";
import { useCreateIssueSchedule, useCancelIssueSchedule } from "@multica/core/issues/mutations";
import { ApiError } from "@multica/core/api/client";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Calendar } from "@multica/ui/components/ui/calendar";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { useT } from "../../i18n";

interface ScheduleRunDialogProps {
  issue: Issue;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** The next clean half-hour mark, so a freshly opened dialog defaults to a
 *  sane near-future time instead of "right now" (which the past-time guard
 *  would immediately reject once the picker re-renders a second later). */
function nextHalfHour(): Date {
  const d = new Date();
  d.setSeconds(0, 0);
  const minutes = d.getMinutes();
  if (minutes < 30) {
    d.setMinutes(30);
  } else {
    d.setMinutes(0);
    d.setHours(d.getHours() + 1);
  }
  return d;
}

function toTimeInputValue(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

const TODAY_MIDNIGHT = () => {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d;
};

/**
 * "Schedule run" dialog (#5927): pick a one-time future instant for the
 * issue's current assignee to run at, or — when one is already pending —
 * show it and offer to cancel. Mirrors the trigger-time picker in
 * autopilots/components/schedule-editor for the date/time controls, but is
 * deliberately much smaller: a one-shot instant has no cron expression, no
 * recurrence, no timezone picker (the browser's own local time is enough —
 * the server only ever sees the resulting UTC instant).
 *
 * Lazily mounted by its caller (only while open), matching SaveViewDialog /
 * AssigneePicker — so every open starts from a fresh "next half hour"
 * default instead of carrying stale picker state across a long-lived
 * parent.
 */
export function ScheduleRunDialog({ issue, open, onOpenChange }: ScheduleRunDialogProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const hasAssignee = Boolean(issue.assignee_type && issue.assignee_id);

  const scheduleQuery = useQuery(issueScheduleOptions(wsId, issue.id));
  const createSchedule = useCreateIssueSchedule(issue.id);
  const cancelSchedule = useCancelIssueSchedule(issue.id);

  const [initialPick] = useState(nextHalfHour);
  const [date, setDate] = useState<Date>(initialPick);
  const [time, setTime] = useState<string>(() => toTimeInputValue(initialPick));

  const runAtLocal = useMemo(() => {
    const parts = time.split(":");
    const hh = Number.parseInt(parts[0] ?? "0", 10);
    const mm = Number.parseInt(parts[1] ?? "0", 10);
    const d = new Date(date);
    d.setHours(Number.isNaN(hh) ? 0 : hh, Number.isNaN(mm) ? 0 : mm, 0, 0);
    return d;
  }, [date, time]);
  const isPast = runAtLocal.getTime() <= Date.now();

  const pending = scheduleQuery.data ?? null;

  const handleSchedule = () => {
    if (isPast || !hasAssignee) return;
    createSchedule.mutate(runAtLocal.toISOString(), {
      onSuccess: () => {
        toast.success(t(($) => $.schedule_dialog.toast_scheduled));
        onOpenChange(false);
      },
      onError: (err) => {
        if (err instanceof ApiError && err.status === 409) {
          toast.error(t(($) => $.schedule_dialog.toast_conflict));
          return;
        }
        toast.error(t(($) => $.schedule_dialog.toast_failed));
      },
    });
  };

  const handleCancelSchedule = () => {
    cancelSchedule.mutate(undefined, {
      onSuccess: () => {
        toast.success(t(($) => $.schedule_dialog.toast_cancelled));
      },
      onError: () => {
        toast.error(t(($) => $.schedule_dialog.toast_failed));
      },
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.schedule_dialog.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.schedule_dialog.description)}</DialogDescription>
        </DialogHeader>

        {!hasAssignee ? (
          <p className="text-body text-muted-foreground">
            {t(($) => $.schedule_dialog.no_assignee)}
          </p>
        ) : scheduleQuery.isLoading ? (
          <div className="py-6 text-center text-body text-muted-foreground">
            {t(($) => $.schedule_dialog.loading)}
          </div>
        ) : pending ? (
          <p className="text-body">
            {t(($) => $.schedule_dialog.pending_description, {
              time: new Date(pending.run_at).toLocaleString(),
            })}
          </p>
        ) : (
          <div className="space-y-4">
            <div className="space-y-1.5">
              <div className="text-caption font-medium text-muted-foreground">
                {t(($) => $.schedule_dialog.date_label)}
              </div>
              <Calendar
                mode="single"
                selected={date}
                onSelect={(d) => {
                  if (d) setDate(d);
                }}
                disabled={{ before: TODAY_MIDNIGHT() }}
              />
            </div>
            <div className="space-y-1.5">
              <div className="text-caption font-medium text-muted-foreground">
                {t(($) => $.schedule_dialog.time_label)}
              </div>
              <TimeInput value={time} onChange={setTime} />
            </div>
            {isPast && (
              <p className="text-caption text-destructive">
                {t(($) => $.schedule_dialog.past_time_error)}
              </p>
            )}
          </div>
        )}

        <DialogFooter>
          {pending ? (
            <>
              <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
                {t(($) => $.schedule_dialog.close_button)}
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={handleCancelSchedule}
                disabled={cancelSchedule.isPending}
              >
                {t(($) => $.schedule_dialog.cancel_schedule_button)}
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
                {t(($) => $.schedule_dialog.cancel_button)}
              </Button>
              <Button
                size="sm"
                onClick={handleSchedule}
                disabled={!hasAssignee || isPast || createSchedule.isPending}
              >
                {t(($) => $.schedule_dialog.schedule_button)}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
