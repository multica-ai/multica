"use client";

import { useEffect, useRef, useState } from "react";
import { CalendarDays, Clock } from "lucide-react";
import { Calendar } from "@/components/ui/calendar";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";

// ── Constants ─────────────────────────────────────────────────────────────────

const DEFAULT_HOUR = 8;
const HOURS = Array.from({ length: 24 }, (_, i) => i);
// All 60 minutes when seconds mode is on; 5-min increments otherwise
const MINUTES_ALL = Array.from({ length: 60 }, (_, i) => i);
const MINUTES_5 = Array.from({ length: 12 }, (_, i) => i * 5);
const SECONDS_ALL = Array.from({ length: 60 }, (_, i) => i);

// ── Helpers ───────────────────────────────────────────────────────────────────

function parseDateValue(value: string | null): Date | undefined {
  if (!value) return undefined;
  const d = new Date(value);
  return isNaN(d.getTime()) ? undefined : d;
}

function getInitialDraft(value: string | null): Date {
  const parsed = parseDateValue(value);
  if (parsed) return parsed;
  const d = new Date();
  d.setHours(DEFAULT_HOUR, 0, 0, 0);
  return d;
}

function formatTimeInput(date: Date | undefined, showSeconds: boolean): string {
  if (!date) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  const base = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  return showSeconds ? `${base}:${pad(date.getSeconds())}` : base;
}

function parseTimeInput(value: string, showSeconds: boolean): { hour: number; minute: number; second: number } | null {
  const pattern = showSeconds ? /^(\d{1,2}):(\d{2}):(\d{2})$/ : /^(\d{1,2}):(\d{2})$/;
  const match = value.match(pattern);
  if (!match) return null;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  const second = showSeconds ? Number(match[3]) : 0;
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59) return null;
  return { hour, minute, second };
}

/** Formats an ISO string for display. Shows seconds when showSeconds is true. */
function formatDisplayValue(value: string | null, showSeconds: boolean): string {
  if (!value) return "";
  return new Date(value).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    ...(showSeconds ? { second: "2-digit" } : {}),
  });
}

// ── Component ─────────────────────────────────────────────────────────────────

export interface DateTimePickerProps {
  /** ISO 8601 string or null (no value). */
  value: string | null;
  /** Called with an ISO string when Apply is clicked, or null when Clear is clicked. */
  onChange: (iso: string | null) => void;
  /** Placeholder text shown when value is null. */
  placeholder?: string;
  /** If true, the Clear button is not shown. */
  required?: boolean;
  /** Disabled state. */
  disabled?: boolean;
  /** Popover alignment. */
  align?: "start" | "center" | "end";
  /** Show seconds scroll column and include seconds in display/input. Default false. */
  showSeconds?: boolean;
}

/**
 * A custom date-time picker that matches the IssueDateTimePicker UX:
 * calendar + hour/minute(/second) scroll lists + free-form time input + Apply/Clear.
 *
 * Pass `showSeconds` to enable second-level precision (scroll column + HH:mm:ss input).
 * Accepts and emits ISO 8601 strings; the internal draft is a `Date` object.
 */
export function DateTimePicker({
  value,
  onChange,
  placeholder = "Pick date & time",
  required = false,
  disabled = false,
  align = "start",
  showSeconds = false,
}: DateTimePickerProps) {
  const [open, setOpen] = useState(false);
  const [draftDate, setDraftDate] = useState<Date>(() => getInitialDraft(value));
  const [timeInput, setTimeInput] = useState(() => formatTimeInput(getInitialDraft(value), showSeconds));
  const hourRef = useRef<HTMLDivElement | null>(null);
  const minuteRef = useRef<HTMLDivElement | null>(null);
  const secondRef = useRef<HTMLDivElement | null>(null);

  const MINUTES = showSeconds ? MINUTES_ALL : MINUTES_5;

  // Reset draft when popover opens with the latest external value.
  useEffect(() => {
    if (open) {
      const next = getInitialDraft(value);
      setDraftDate(next);
      setTimeInput(formatTimeInput(next, showSeconds));
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  // Sync time input text whenever draft date changes.
  useEffect(() => {
    setTimeInput(formatTimeInput(draftDate, showSeconds));
  }, [draftDate, showSeconds]);

  // Scroll selected hour/minute/second into view when popover opens.
  useEffect(() => {
    if (!open) return;
    const id = window.requestAnimationFrame(() => {
      hourRef.current
        ?.querySelector<HTMLButtonElement>(`[data-time-unit="hour"][data-time-value="${draftDate.getHours()}"]`)
        ?.scrollIntoView({ block: "nearest" });
      minuteRef.current
        ?.querySelector<HTMLButtonElement>(`[data-time-unit="minute"][data-time-value="${draftDate.getMinutes()}"]`)
        ?.scrollIntoView({ block: "nearest" });
      secondRef.current
        ?.querySelector<HTMLButtonElement>(`[data-time-unit="second"][data-time-value="${draftDate.getSeconds()}"]`)
        ?.scrollIntoView({ block: "nearest" });
    });
    return () => window.cancelAnimationFrame(id);
  }, [open, draftDate]);

  const handleDateSelect = (selected: Date | undefined) => {
    if (!selected) return;
    setDraftDate((cur) => {
      const next = new Date(selected);
      next.setHours(cur.getHours(), cur.getMinutes(), showSeconds ? cur.getSeconds() : 0, 0);
      return next;
    });
  };

  const handleTimeUnitChange = (type: "hour" | "minute" | "second", v: number) => {
    setDraftDate((cur) => {
      const next = new Date(cur);
      if (type === "hour") next.setHours(v);
      else if (type === "minute") next.setMinutes(v);
      else next.setSeconds(v, 0);
      if (!showSeconds) next.setSeconds(0, 0);
      return next;
    });
  };

  const handleTimeInputChange = (raw: string) => {
    const maxLen = showSeconds ? 8 : 5;
    const sanitized = raw.replace(/[^\d:]/g, "").slice(0, maxLen);
    setTimeInput(sanitized);
    const parsed = parseTimeInput(sanitized, showSeconds);
    if (!parsed) return;
    setDraftDate((cur) => {
      const next = new Date(cur);
      next.setHours(parsed.hour, parsed.minute, parsed.second, 0);
      return next;
    });
  };

  const handleApply = () => {
    onChange(draftDate.toISOString());
    setOpen(false);
  };

  const handleClear = () => {
    onChange(null);
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={disabled ? undefined : setOpen}>
      <PopoverTrigger
        disabled={disabled}
        className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
      >
        <CalendarDays className="size-3.5 text-muted-foreground shrink-0" />
        {value ? (
          <span className="text-sm">{formatDisplayValue(value, showSeconds)}</span>
        ) : (
          <span className="text-sm text-muted-foreground">{placeholder}</span>
        )}
      </PopoverTrigger>

      <PopoverContent
        align={align}
        className="w-[min(22rem,calc(100vw-1rem))] max-h-[calc(100dvh-1rem)] overflow-y-auto p-0 sm:w-auto sm:max-h-none sm:overflow-visible"
      >
        {/* Calendar */}
        <Calendar
          mode="single"
          selected={draftDate}
          onSelect={handleDateSelect}
          initialFocus
        />

        {/* Time text input */}
        <div className="border-t p-2">
          <div className="flex items-center gap-2 rounded-lg border bg-background px-3 py-2">
            <Clock className="size-4 text-primary" />
            <Input
              value={timeInput}
              onChange={(e) => handleTimeInputChange(e.target.value)}
              placeholder={showSeconds ? "HH:mm:ss" : "HH:mm"}
              inputMode="numeric"
              className="h-auto border-0 bg-transparent px-0 py-0 text-base text-primary shadow-none focus-visible:ring-0"
            />
          </div>
        </div>

        {/* Hour / minute / (optional) second scroll lists */}
        <div className="flex border-t">
          <ScrollArea className={`h-40 sm:h-56 border-r ${showSeconds ? "w-1/3" : "w-1/2"}`}>
            <div ref={hourRef} className="flex flex-col p-2">
              {HOURS.map((h) => (
                <Button
                  key={h}
                  size="sm"
                  data-time-unit="hour"
                  data-time-value={h}
                  variant={draftDate.getHours() === h ? "default" : "ghost"}
                  className="w-full shrink-0 justify-start font-normal"
                  onClick={() => handleTimeUnitChange("hour", h)}
                >
                  {String(h).padStart(2, "0")}
                </Button>
              ))}
            </div>
          </ScrollArea>
          <ScrollArea className={`h-40 sm:h-56 ${showSeconds ? "w-1/3 border-r" : "w-1/2"}`}>
            <div ref={minuteRef} className="flex flex-col p-2">
              {MINUTES.map((m) => (
                <Button
                  key={m}
                  size="sm"
                  data-time-unit="minute"
                  data-time-value={m}
                  variant={draftDate.getMinutes() === m ? "default" : "ghost"}
                  className="w-full shrink-0 justify-start font-normal"
                  onClick={() => handleTimeUnitChange("minute", m)}
                >
                  {String(m).padStart(2, "0")}
                </Button>
              ))}
            </div>
          </ScrollArea>
          {showSeconds && (
            <ScrollArea className="h-40 w-1/3 sm:h-56">
              <div ref={secondRef} className="flex flex-col p-2">
                {SECONDS_ALL.map((s) => (
                  <Button
                    key={s}
                    size="sm"
                    data-time-unit="second"
                    data-time-value={s}
                    variant={draftDate.getSeconds() === s ? "default" : "ghost"}
                    className="w-full shrink-0 justify-start font-normal"
                    onClick={() => handleTimeUnitChange("second", s)}
                  >
                    {String(s).padStart(2, "0")}
                  </Button>
                ))}
              </div>
            </ScrollArea>
          )}
        </div>

        {/* Apply / Clear */}
        <div className="sticky bottom-0 flex items-center justify-between gap-2 border-t bg-popover px-3 py-2">
          {!required && (
            <Button
              variant="ghost"
              size="xs"
              className="text-muted-foreground hover:text-foreground"
              onClick={handleClear}
            >
              Clear
            </Button>
          )}
          <Button size="xs" className="ml-auto" onClick={handleApply}>
            Apply
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
