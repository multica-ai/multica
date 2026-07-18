"use client";

import * as React from "react";
import { Clock } from "lucide-react";

import { cn } from "@multica/ui/lib/utils";

// Adapted from openstatusHQ/time-picker (MIT).
// Segmented HH:MM input with keyboard arrow increment / digit typing.

function pad2(n: number): string {
  return n.toString().padStart(2, "0");
}

function getValidNumber(
  raw: string,
  { max, min = 0, loop = false }: { max: number; min?: number; loop?: boolean },
): string {
  let n = parseInt(raw, 10);
  if (isNaN(n)) return pad2(min);
  if (!loop) {
    if (n > max) n = max;
    if (n < min) n = min;
  } else {
    if (n > max) n = min;
    if (n < min) n = max;
  }
  return pad2(n);
}

function arrowValue(current: string, step: number, min: number, max: number): string {
  const n = parseInt(current, 10);
  if (isNaN(n)) return pad2(min);
  return getValidNumber(String(n + step), { max, min, loop: true });
}

function splitTime(value: string, hourMin: number): { hh: string; mm: string } {
  const [rawH, rawM] = (value || "").split(":");
  const hh = getValidNumber(rawH ?? "0", { max: 23, min: hourMin });
  const mm = getValidNumber(rawM ?? "0", { max: 59 });
  return { hh, mm };
}

interface SegmentInputProps {
  value: string;
  /** The segment's own range. Arrow keys wrap inside it, and typed digits are
   *  clamped to it, so a segment whose range is narrowed by its neighbour — a
   *  window's end hour, which can never precede its start — can neither be
   *  arrowed nor typed out of bounds. */
  min: number;
  max: number;
  /** What Backspace clears the segment to. Defaults to `min` — the neutral value
   *  for a segment whose range starts at the field's own floor. A window's end
   *  hour is not such a segment: its `min` is wherever the start currently sits,
   *  so clearing it to `min` would collapse the window onto the start instead of
   *  opening it to the end of the day. */
  clearTo?: number;
  onValueChange: (next: string) => void;
  onLeftFocus?: () => void;
  onRightFocus?: () => void;
  disabled?: boolean;
  ariaLabel: string;
  /** `data-slot` for the segment itself. The segments are what a container's
   *  focus styling has to see: they, not the wrapper, are what gets focused. */
  slot?: string;
  autoFocus?: boolean;
}

const SegmentInput = React.forwardRef<HTMLInputElement, SegmentInputProps>(function SegmentInput(
  { value, min, max, clearTo, onValueChange, onLeftFocus, onRightFocus, disabled, ariaLabel, slot, autoFocus },
  ref,
) {
  // Two-digit typing window: the first digit pads with a leading 0; a second
  // digit within 2s completes the pair. The first digit is remembered here
  // rather than read back from `value`, so an owner that clamps the value
  // between the two keystrokes cannot corrupt the number being typed.
  const [pendingFirst, setPendingFirst] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (pendingFirst === null) return;
    const t = setTimeout(() => setPendingFirst(null), 2000);
    return () => clearTimeout(t);
  }, [pendingFirst]);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Tab") return;
    if (e.key === "ArrowRight") {
      e.preventDefault();
      onRightFocus?.();
      return;
    }
    if (e.key === "ArrowLeft") {
      e.preventDefault();
      onLeftFocus?.();
      return;
    }
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault();
      const step = e.key === "ArrowUp" ? 1 : -1;
      onValueChange(arrowValue(value, step, min, max));
      setPendingFirst(null);
      return;
    }
    if (e.key >= "0" && e.key <= "9") {
      e.preventDefault();
      if (pendingFirst !== null) {
        onValueChange(getValidNumber(pendingFirst + e.key, { max, min }));
        setPendingFirst(null);
        onRightFocus?.();
      } else {
        // The first digit is clamped for display but remembered raw: in a
        // segment starting at 09, a typed "1" must still show something legal
        // while staying the tens digit of the 15 the user is halfway through.
        onValueChange(getValidNumber("0" + e.key, { max, min }));
        setPendingFirst(e.key);
      }
      return;
    }
    if (e.key === "Backspace" || e.key === "Delete") {
      e.preventDefault();
      onValueChange(pad2(clearTo ?? min));
      setPendingFirst(null);
    }
  };

  return (
    <input
      ref={ref}
      data-slot={slot}
      // Caller-gated: only set when the user just switched into this control,
      // never on first mount (see TimeInput's autoFocus doc).
      autoFocus={autoFocus}
      type="text"
      inputMode="numeric"
      maxLength={2}
      value={value}
      disabled={disabled}
      aria-label={ariaLabel}
      onChange={() => {
        // Fully controlled by keydown; ignore native onChange.
      }}
      onKeyDown={handleKeyDown}
      onFocus={(e) => e.currentTarget.select()}
      // The two-digit window belongs to one visit to this segment. Left open, a
      // "1" typed here and a "5" typed after a detour through another control
      // would pair up into 15.
      onBlur={() => setPendingFirst(null)}
      className="w-7 bg-transparent text-center text-sm tabular-nums outline-none caret-transparent focus:text-foreground disabled:opacity-50"
    />
  );
});

export interface TimeInputProps {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  className?: string;
  showIcon?: boolean;
  /** Render only the hour segment. Used where the minute has no meaning, such as
   *  the bounds of an hour-granular window. */
  hourOnly?: boolean;
  /** Earliest hour this field may hold. The hour segment wraps and clamps within
   *  [hourMin, 23] instead of [0, 23] — a window's end, which cannot precede its
   *  start, arrows round to 23 rather than sticking at the start hour. */
  hourMin?: number;
  /** What Backspace clears the hour to, when that is not `hourMin` (see
   *  SegmentInput's clearTo). */
  hourClearTo?: number;
  /** Accessible names for the two segments. This package holds no translations,
   *  so callers that render several time fields side by side — a window's start
   *  and end, say — pass localized names that tell them apart. */
  hourLabel?: string;
  minuteLabel?: string;
  /** `data-slot` for the hour and minute segments, for a container that styles
   *  itself from what is focused inside it (see InputGroupTimeInput). */
  segmentSlot?: string;
  /** Focus the hour segment on mount. Callers gate this so it only fires when the
   *  user just revealed this field, never on the first render of a form. */
  autoFocus?: boolean;
}

export function TimeInput({
  value,
  onChange,
  disabled,
  className,
  showIcon = true,
  hourOnly = false,
  hourMin = 0,
  hourClearTo,
  hourLabel = "Hour",
  minuteLabel = "Minute",
  segmentSlot,
  autoFocus,
}: TimeInputProps) {
  const { hh, mm } = splitTime(value, hourMin);
  const hourRef = React.useRef<HTMLInputElement>(null);
  const minuteRef = React.useRef<HTMLInputElement>(null);

  const setHour = (next: string) => onChange(`${next}:${mm}`);
  const setMinute = (next: string) => onChange(`${hh}:${next}`);

  return (
    <div
      data-slot="time-input"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          hourRef.current?.focus();
        }
      }}
      className={cn(
        "flex h-8 items-center gap-1 rounded-lg border border-input bg-transparent px-2.5 text-sm transition-colors",
        "focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
        "dark:bg-input/30",
        disabled && "pointer-events-none cursor-not-allowed opacity-50",
        className,
      )}
    >
      {showIcon && (
        <Clock className="pointer-events-none size-3.5 shrink-0 text-muted-foreground" />
      )}
      <SegmentInput
        ref={hourRef}
        min={hourMin}
        max={23}
        clearTo={hourClearTo}
        value={hh}
        onValueChange={setHour}
        // Nowhere to advance to when the minute is off: focusing a disabled
        // input is a no-op, and the keypress that asked for it is already
        // swallowed, so the caret would appear stuck.
        onRightFocus={hourOnly ? undefined : () => minuteRef.current?.focus()}
        disabled={disabled}
        ariaLabel={hourLabel}
        slot={segmentSlot}
        autoFocus={autoFocus}
      />
      {/* hourOnly drops the minute segment and its colon entirely, rather than
          keeping an inert one for stable geometry: where the bounds are whole
          hours, a greyed-out :00 is only wasted width. The field narrows on the
          unit switch that sets this — the honest signal that minute-granular
          bounds are gone. */}
      {!hourOnly && (
        <>
          {/* -translate-y-[1.5px]: the digits are cap-height figures whose
              optical centre sits above the colon's two dots, so `items-center` —
              which lines up the boxes, not the ink — leaves the colon reading
              low. Nudge it up to the digits' centre. Transform, not margin, so
              it shifts nothing else. */}
          <span className="pointer-events-none -translate-y-[1.5px] select-none text-muted-foreground">
            :
          </span>
          <SegmentInput
            ref={minuteRef}
            min={0}
            max={59}
            value={mm}
            onValueChange={setMinute}
            onLeftFocus={() => hourRef.current?.focus()}
            disabled={disabled}
            ariaLabel={minuteLabel}
            slot={segmentSlot}
          />
        </>
      )}
    </div>
  );
}
