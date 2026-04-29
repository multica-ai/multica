"use client";

import { useEffect, useState } from "react";
import { CalendarDays, Clock } from "lucide-react";
import type { UpdateIssueRequest } from "@/shared/types";
import { Calendar } from "@/components/ui/calendar";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";

export type IssueDateTimeField = "start_date" | "end_date";

const DATETIME_FIELD_LABELS: Record<IssueDateTimeField, string> = {
  start_date: "Start date",
  end_date: "End date",
};

const HOURS = Array.from({ length: 24 }, (_, index) => 23 - index);
const MINUTES = Array.from({ length: 12 }, (_, index) => index * 5);
const DATE_SHORTCUTS = [
  { label: "Today", getDate: () => new Date() },
  {
    label: "Tomorrow",
    getDate: () => {
      const nextDate = new Date();
      nextDate.setDate(nextDate.getDate() + 1);
      return nextDate;
    },
  },
  {
    label: "Next week",
    getDate: () => {
      const nextDate = new Date();
      nextDate.setDate(nextDate.getDate() + 7);
      return nextDate;
    },
  },
  {
    label: "Next month",
    getDate: () => {
      const nextDate = new Date();
      nextDate.setMonth(nextDate.getMonth() + 1);
      return nextDate;
    },
  },
] as const;

function buildIssueDateTimeUpdate(field: IssueDateTimeField, value: string | null): Partial<UpdateIssueRequest> {
  switch (field) {
    case "start_date":
      return { start_date: value };
    default:
      return { end_date: value };
  }
}

function parseDateTimeValue(value: string | null): Date | undefined {
  if (!value) return undefined;

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return undefined;

  return date;
}

function startOfDay(date: Date): Date {
  const nextDate = new Date(date);
  nextDate.setHours(0, 0, 0, 0);
  return nextDate;
}

function getDraftDate(value: string | null): Date {
  return parseDateTimeValue(value) ?? startOfDay(new Date());
}

function formatDateTime(value: string | null): string {
  if (!value) return "";

  return new Date(value).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function formatTimeInput(date: Date | undefined): string {
  if (!date) return "";

  return `${date.getHours().toString().padStart(2, "0")}:${date.getMinutes().toString().padStart(2, "0")}`;
}

function parseTimeInput(value: string): { hour: number; minute: number } | null {
  const match = value.match(/^(\d{1,2}):(\d{2})$/);
  if (!match) {
    return null;
  }

  const hour = Number(match[1]);
  const minute = Number(match[2]);

  if (!Number.isInteger(hour) || !Number.isInteger(minute) || hour < 0 || hour > 23 || minute < 0 || minute > 59) {
    return null;
  }

  return { hour, minute };
}

export function IssueDateTimePicker({
  field,
  dateTimeValue,
  onUpdate,
  trigger: customTrigger,
}: {
  field: IssueDateTimeField;
  dateTimeValue: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactElement;
}) {
  const [open, setOpen] = useState(false);
  const [draftDate, setDraftDate] = useState<Date | undefined>(() => getDraftDate(dateTimeValue));
  const [timeInput, setTimeInput] = useState(() => formatTimeInput(getDraftDate(dateTimeValue)));

  useEffect(() => {
    if (!open) {
      const nextDate = getDraftDate(dateTimeValue);
      setDraftDate(nextDate);
      setTimeInput(formatTimeInput(nextDate));
    }
  }, [dateTimeValue, open]);

  useEffect(() => {
    setTimeInput(formatTimeInput(draftDate));
  }, [draftDate]);

  const handleDateSelect = (selectedDate: Date | undefined) => {
    if (!selectedDate) {
      return;
    }

    setDraftDate((current) => {
      const nextDate = new Date(selectedDate);
      if (current) {
        nextDate.setHours(current.getHours(), current.getMinutes(), 0, 0);
      } else {
        nextDate.setHours(0, 0, 0, 0);
      }
      return nextDate;
    });
  };

  const handleTimeChange = (type: "hour" | "minute", value: number) => {
    setDraftDate((current) => {
      if (!current) {
        return current;
      }

      const nextDate = new Date(current);
      if (type === "hour") {
        nextDate.setHours(value);
      } else {
        nextDate.setMinutes(value);
      }
      nextDate.setSeconds(0, 0);
      return nextDate;
    });
  };

  const handleTimeInputChange = (value: string) => {
    const sanitized = value.replace(/[^\d:]/g, "").slice(0, 5);
    setTimeInput(sanitized);

    const parsed = parseTimeInput(sanitized);
    if (!parsed) {
      return;
    }

    setDraftDate((current) => {
      const nextDate = current ? new Date(current) : new Date();
      nextDate.setHours(parsed.hour, parsed.minute, 0, 0);
      return nextDate;
    });
  };

  const emptyLabel = DATETIME_FIELD_LABELS[field];

  const handleShortcutSelect = (getShortcutDate: () => Date) => {
    setDraftDate((current) => {
      const nextDate = startOfDay(getShortcutDate());
      if (current) {
        nextDate.setHours(current.getHours(), current.getMinutes(), 0, 0);
      }
      return nextDate;
    });
  };

  const handleApply = () => {
    onUpdate(buildIssueDateTimeUpdate(field, draftDate ? draftDate.toISOString() : null));
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) {
          const nextDate = getDraftDate(dateTimeValue);
          setDraftDate(nextDate);
          setTimeInput(formatTimeInput(nextDate));
        }
      }}
    >
      {customTrigger ? (
        <PopoverTrigger render={customTrigger} />
      ) : (
        <PopoverTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors">
          <>
            <CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />
            {dateTimeValue ? (
              <span>{formatDateTime(dateTimeValue)}</span>
            ) : (
              <span className="text-muted-foreground">{emptyLabel}</span>
            )}
          </>
        </PopoverTrigger>
      )}
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar
          mode="single"
          selected={draftDate}
          onSelect={handleDateSelect}
          initialFocus
        />
        <div className="grid grid-cols-2 gap-2 border-t p-2">
          {DATE_SHORTCUTS.map((shortcut) => (
            <Button
              key={shortcut.label}
              variant="ghost"
              size="sm"
              className="justify-start font-normal"
              onClick={() => handleShortcutSelect(shortcut.getDate)}
            >
              {shortcut.label}
            </Button>
          ))}
        </div>
        <div className="border-t p-2">
          <div className="flex items-center gap-2 rounded-lg border bg-background px-3 py-2">
            <Clock className="h-4 w-4 text-primary" />
            <Input
              value={timeInput}
              onChange={(event) => handleTimeInputChange(event.target.value)}
              placeholder="HH:mm"
              inputMode="numeric"
              className="h-auto border-0 bg-transparent px-0 py-0 text-base text-primary shadow-none focus-visible:ring-0"
            />
          </div>
        </div>
        <div className="flex border-t">
          <ScrollArea className="h-56 w-1/2 border-r">
            <div className="flex flex-col p-2">
              {HOURS.map((hour) => (
                <Button
                  key={hour}
                  size="sm"
                  variant={draftDate && draftDate.getHours() === hour ? "default" : "ghost"}
                  className="w-full shrink-0 justify-start font-normal"
                  disabled={!draftDate}
                  onClick={() => handleTimeChange("hour", hour)}
                >
                  {hour.toString().padStart(2, "0")}
                </Button>
              ))}
            </div>
          </ScrollArea>
          <ScrollArea className="h-56 w-1/2">
            <div className="flex flex-col p-2">
              {MINUTES.map((minute) => (
                <Button
                  key={minute}
                  size="sm"
                  variant={draftDate && draftDate.getMinutes() === minute ? "default" : "ghost"}
                  className="w-full shrink-0 justify-start font-normal"
                  disabled={!draftDate}
                  onClick={() => handleTimeChange("minute", minute)}
                >
                  {minute.toString().padStart(2, "0")}
                </Button>
              ))}
            </div>
          </ScrollArea>
        </div>
        <div className="flex items-center justify-between gap-2 border-t px-3 py-2">
          <Button
            variant="ghost"
            size="xs"
            disabled={!draftDate}
            onClick={() => {
              onUpdate(buildIssueDateTimeUpdate(field, null));
              setOpen(false);
            }}
            className="text-muted-foreground hover:text-foreground"
          >
            Clear date
          </Button>
          <Button size="xs" disabled={!draftDate} onClick={handleApply}>
            Apply
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}