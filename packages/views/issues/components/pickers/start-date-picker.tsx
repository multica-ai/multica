"use client";

import { useState } from "react";
import { CalendarClock, Clock } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import {
  isoToLocalDate,
  localDateTimeToIso,
  formatScheduleDate,
  DEFAULT_START_TIME,
} from "@multica/core/issues/date";
import { Calendar } from "@multica/ui/components/ui/calendar";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../i18n";

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}
function toTimeValue(d: Date): string {
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}
const DEFAULT_TIME_VALUE = `${pad2(DEFAULT_START_TIME.h)}:${pad2(DEFAULT_START_TIME.m)}`;

export function StartDatePicker({
  startDate,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align = "start",
  defaultOpen = false,
}: {
  startDate: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /** Open the popover on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
}) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const date = isoToLocalDate(startDate);
  // Time-of-day, seeded from the stored value or the field default. The popover
  // stays open after picking a day so the user can adjust the time; clicking
  // outside closes it. Each change emits an instant via localDateTimeToIso.
  const [time, setTime] = useState<string>(date ? toTimeValue(date) : DEFAULT_TIME_VALUE);

  const emit = (day: Date, timeStr: string) => {
    const [h, m] = timeStr.split(":").map(Number);
    const combined = new Date(
      day.getFullYear(),
      day.getMonth(),
      day.getDate(),
      Number.isFinite(h) ? h : 0,
      Number.isFinite(m) ? m : 0,
    );
    onUpdate({ start_date: localDateTimeToIso(combined) });
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors"}
        render={triggerRender}
      >
        {customTrigger ?? (
          <>
            <CalendarClock className="h-3.5 w-3.5 text-muted-foreground" />
            {date ? (
              <span>
                {formatScheduleDate(startDate, "en-US")}
              </span>
            ) : (
              <span className="text-muted-foreground">{t(($) => $.pickers.start_date.trigger_label)}</span>
            )}
          </>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <Calendar
          mode="single"
          selected={date}
          onSelect={(d: Date | undefined) => {
            if (d) emit(d, time);
          }}
        />
        <div className="flex items-center gap-2 border-t px-3 py-2">
          <Clock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <input
            type="time"
            value={time}
            onChange={(e) => {
              setTime(e.target.value);
              if (date) emit(date, e.target.value);
            }}
            className="rounded border border-input bg-transparent px-1.5 py-0.5 text-sm outline-none focus:ring-1 focus:ring-ring"
          />
          {date && (
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                onUpdate({ start_date: null });
                setOpen(false);
              }}
              className="ml-auto text-muted-foreground hover:text-foreground"
            >
              {t(($) => $.pickers.start_date.clear_action)}
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
