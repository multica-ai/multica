"use client";

import { useState } from "react";
import {
  toDateOnly,
  dateOnlyToLocalDate,
  formatDateOnly,
  isPastDateOnly,
} from "@multica/core/issues/date";
import { Calendar } from "@multica/ui/components/ui/calendar";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";

/**
 * Entity-agnostic calendar-day picker: the shared behaviour behind every
 * start/due-date pill (issues, projects, …). It owns the Popover + Calendar +
 * clear wiring and the calendar-day transport ("YYYY-MM-DD", no timezone shift,
 * via @multica/core/issues/date); each entity wraps it to supply only the field
 * name (through `onChange`), the icon, and the localized copy. Keeping this in
 * one place stops the per-entity pills from drifting in behaviour or display
 * formatting.
 */
export function DateOnlyPicker({
  value,
  onChange,
  icon,
  placeholder,
  clearLabel,
  highlightOverdue = false,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align = "start",
  defaultOpen = false,
}: {
  /** Selected calendar day ("YYYY-MM-DD") or null. */
  value: string | null;
  /** Emits the new calendar day, or null when cleared. */
  onChange: (value: string | null) => void;
  /** Trigger icon — the entity picks CalendarClock (start) vs CalendarDays (due). */
  icon: React.ReactNode;
  /** Placeholder label shown when no date is set. */
  placeholder: string;
  /** Label for the "clear date" action inside the popover. */
  clearLabel: string;
  /** Paint the value with `text-destructive` when it is in the past (due dates). */
  highlightOverdue?: boolean;
  /** Fully custom trigger contents (replaces the icon + date/placeholder). */
  trigger?: React.ReactNode;
  /** Custom trigger element (e.g. a pill button). */
  triggerRender?: React.ReactElement;
  /** Controlled open state — lets a ⋯ overflow menu reveal + open the pill. */
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /** Open the popover on first mount (progressive-disclosure sidebars). */
  defaultOpen?: boolean;
}) {
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const date = dateOnlyToLocalDate(value);
  const overdue = highlightOverdue && isPastDateOnly(value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors"}
        render={triggerRender}
      >
        {customTrigger ?? (
          <>
            {icon}
            {date ? (
              <span className={overdue ? "text-destructive" : ""}>
                {formatDateOnly(value, { month: "short", day: "numeric" }, "en-US")}
              </span>
            ) : (
              <span className="text-muted-foreground">{placeholder}</span>
            )}
          </>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <Calendar
          mode="single"
          selected={date}
          onSelect={(d: Date | undefined) => {
            onChange(d ? toDateOnly(d) : null);
            setOpen(false);
          }}
        />
        {date && (
          <div className="border-t px-3 py-2">
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                onChange(null);
                setOpen(false);
              }}
              className="text-muted-foreground hover:text-foreground"
            >
              {clearLabel}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
