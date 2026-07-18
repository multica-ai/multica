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
import { DeferredPopup } from "./deferred-popup";

/**
 * Default class of the date pill trigger — shared with the deferred lookalike
 * trigger so the swap on first interaction is pixel-identical.
 */
const DATE_TRIGGER_CLASS =
  "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors";

interface DateOnlyPickerProps {
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
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  /** Controlled open state — lets a ⋯ overflow menu reveal + open the pill. */
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /** Open the popover on first mount (progressive-disclosure sidebars). */
  defaultOpen?: boolean;
}

/**
 * Entity-agnostic calendar-day picker: the shared behaviour behind every
 * start/due-date pill (issues, projects, …). It owns the Popover + Calendar +
 * clear wiring and the calendar-day transport ("YYYY-MM-DD", no timezone shift,
 * via @multica/core/issues/date); each entity wraps it to supply only the field
 * name (through `onChange`), the icon, and the localized copy. Keeping this in
 * one place stops the per-entity pills from drifting in behaviour or display
 * formatting.
 *
 * Uncontrolled usages render a deferred lookalike trigger and mount the
 * Popover + Calendar machinery on first interaction — the default trigger is
 * computable from props alone, so every uncontrolled pill defers. See
 * `DeferredPopup` for why.
 */
export function DateOnlyPicker(props: DateOnlyPickerProps) {
  const canDefer =
    props.open === undefined &&
    props.onOpenChange === undefined &&
    !props.defaultOpen;
  if (!canDefer) {
    return <DateOnlyPickerImpl {...props} />;
  }
  return (
    <DeferredPopup
      trigger={props.trigger ?? <DateTriggerContent {...props} />}
      triggerRender={props.triggerRender}
      triggerClassName={DATE_TRIGGER_CLASS}
    >
      {(open, onOpenChange) => (
        <DateOnlyPickerImpl {...props} open={open} onOpenChange={onOpenChange} />
      )}
    </DeferredPopup>
  );
}

/** Default trigger content, kept identical to what the Impl renders. */
function DateTriggerContent({
  value,
  icon,
  placeholder,
  highlightOverdue = false,
}: Pick<DateOnlyPickerProps, "value" | "icon" | "placeholder" | "highlightOverdue">) {
  const date = dateOnlyToLocalDate(value);
  const overdue = highlightOverdue && isPastDateOnly(value);
  return (
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
  );
}

function DateOnlyPickerImpl({
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
}: DateOnlyPickerProps) {
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const date = dateOnlyToLocalDate(value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : DATE_TRIGGER_CLASS}
        render={triggerRender}
      >
        {customTrigger ?? (
          <DateTriggerContent
            value={value}
            icon={icon}
            placeholder={placeholder}
            highlightOverdue={highlightOverdue}
          />
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
