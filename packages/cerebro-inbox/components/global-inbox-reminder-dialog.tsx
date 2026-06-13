"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useCreateInboxReminder } from "../mutations";
import { nextBusinessDayNineAm, toDateTimeLocalValue } from "../mute-time";

export interface GlobalInboxReminderDialogLabels {
  title: string;
  reminder: string;
  placeholder: string;
  plannedFor: string;
  tomorrowNine: string;
  nextWorkdayNine: string;
  cancel: string;
  plan: string;
  missingText: string;
  futureTime: string;
  planned: string;
  createFailed: string;
}

export const DEFAULT_GLOBAL_INBOX_REMINDER_LABELS: GlobalInboxReminderDialogLabels = {
  title: "New reminder",
  reminder: "Reminder",
  placeholder: "What should you be reminded about?",
  plannedFor: "Planned for",
  tomorrowNine: "Tomorrow 9:00",
  nextWorkdayNine: "Next workday 9:00",
  cancel: "Cancel",
  plan: "Plan",
  missingText: "Write what you want to be reminded about.",
  futureTime: "Pick a future time.",
  planned: "Reminder planned",
  createFailed: "Couldn't create reminder",
};

function nextLocalNineAm() {
  const next = new Date();
  next.setDate(next.getDate() + 1);
  next.setHours(9, 0, 0, 0);
  return next;
}

export function GlobalInboxReminderDialog({
  open,
  onOpenChange,
  labels = DEFAULT_GLOBAL_INBOX_REMINDER_LABELS,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  labels?: GlobalInboxReminderDialogLabels;
  onCreated?: () => void;
}) {
  const [text, setText] = useState("");
  const [planned, setPlanned] = useState(() =>
    toDateTimeLocalValue(nextBusinessDayNineAm()),
  );
  const createReminder = useCreateInboxReminder();

  const handleCreate = () => {
    const plannedAt = new Date(planned);
    if (!text.trim()) {
      toast.error(labels.missingText);
      return;
    }
    if (Number.isNaN(plannedAt.getTime()) || plannedAt.getTime() <= Date.now()) {
      toast.error(labels.futureTime);
      return;
    }
    createReminder.mutate(
      { text: text.trim(), plannedAt },
      {
        onSuccess: () => {
          setText("");
          setPlanned(toDateTimeLocalValue(nextBusinessDayNineAm()));
          onOpenChange(false);
          onCreated?.();
          toast.success(labels.planned);
        },
        onError: () => toast.error(labels.createFailed),
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{labels.title}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 py-1">
          <div className="grid gap-2">
            <Label htmlFor="inbox-reminder-text">{labels.reminder}</Label>
            <Textarea
              id="inbox-reminder-text"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={labels.placeholder}
              className="min-h-24 resize-none"
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="inbox-reminder-planned">{labels.plannedFor}</Label>
            <Input
              id="inbox-reminder-planned"
              type="datetime-local"
              value={planned}
              onChange={(e) => setPlanned(e.target.value)}
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setPlanned(toDateTimeLocalValue(nextLocalNineAm()))}
            >
              {labels.tomorrowNine}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setPlanned(toDateTimeLocalValue(nextBusinessDayNineAm()))}
            >
              {labels.nextWorkdayNine}
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {labels.cancel}
          </Button>
          <Button
            type="button"
            onClick={handleCreate}
            disabled={createReminder.isPending}
          >
            {labels.plan}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
