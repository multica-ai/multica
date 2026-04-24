"use client";

import * as React from "react";
import { Clock } from "lucide-react";

import { cn } from "@multica/ui/lib/utils";

// Adapted from openstatusHQ/time-picker (MIT).
// Segmented HH:MM input with keyboard arrow increment / digit typing.

type Segment = "hours" | "minutes";

function getValidNumber(
  raw: string,
  { max, min = 0, loop = false }: { max: number; min?: number; loop?: boolean },
): string {
  let n = parseInt(raw, 10);
  if (isNaN(n)) return "00";
  if (!loop) {
    if (n > max) n = max;
    if (n < min) n = min;
  } else {
    if (n > max) n = min;
    if (n < min) n = max;
  }
  return n.toString().padStart(2, "0");
}

function arrowValue(current: string, step: number, seg: Segment): string {
  const max = seg === "hours" ? 23 : 59;
  const n = parseInt(current, 10);
  if (isNaN(n)) return "00";
  return getValidNumber(String(n + step), { max, min: 0, loop: true });
}

function splitTime(value: string): { hh: string; mm: string } {
  const [rawH, rawM] = (value || "").split(":");
  const hh = getValidNumber(rawH ?? "0", { max: 23 });
  const mm = getValidNumber(rawM ?? "0", { max: 59 });
  return { hh, mm };
}

interface SegmentInputProps {
  seg: Segment;
  value: string;
  onValueChange: (next: string) => void;
  onLeftFocus?: () => void;
  onRightFocus?: () => void;
  disabled?: boolean;
  ariaLabel: string;
}

const SegmentInput = React.forwardRef<HTMLInputElement, SegmentInputProps>(
  function SegmentInput(
    { seg, value, onValueChange, onLeftFocus, onRightFocus, disabled, ariaLabel },
    ref,
  ) {
    // Two-digit typing window: first digit pads with leading 0; second digit within
    // 2s replaces the leading 0, clamped to segment max. After 2s, reset.
    const [pendingSecond, setPendingSecond] = React.useState(false);

    React.useEffect(() => {
      if (!pendingSecond) return;
      const t = setTimeout(() => setPendingSecond(false), 2000);
      return () => clearTimeout(t);
    }, [pendingSecond]);

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
        onValueChange(arrowValue(value, step, seg));
        setPendingSecond(false);
        return;
      }
      if (e.key >= "0" && e.key <= "9") {
        e.preventDefault();
        const next = pendingSecond
          ? getValidNumber(value.slice(1) + e.key, {
              max: seg === "hours" ? 23 : 59,
            })
          : "0" + e.key;
        onValueChange(next);
        if (pendingSecond) {
          setPendingSecond(false);
          onRightFocus?.();
        } else {
          setPendingSecond(true);
        }
        return;
      }
      if (e.key === "Backspace" || e.key === "Delete") {
        e.preventDefault();
        onValueChange("00");
        setPendingSecond(false);
      }
    };

    return (
      <input
        ref={ref}
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
        className="w-7 bg-transparent text-center text-sm tabular-nums outline-none caret-transparent focus:text-foreground"
      />
    );
  },
);

export interface TimeInputProps {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  className?: string;
  showIcon?: boolean;
  /** Render only the minute segment with an "At :" prefix. Used for hourly schedules. */
  minuteOnly?: boolean;
}

export function TimeInput({
  value,
  onChange,
  disabled,
  className,
  showIcon = true,
  minuteOnly = false,
}: TimeInputProps) {
  const { hh, mm } = splitTime(value);
  const hourRef = React.useRef<HTMLInputElement>(null);
  const minuteRef = React.useRef<HTMLInputElement>(null);

  const setHour = (next: string) => onChange(`${next}:${mm}`);
  const setMinute = (next: string) =>
    onChange(`${minuteOnly ? "00" : hh}:${next}`);

  return (
    <div
      data-slot="time-input"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          (minuteOnly ? minuteRef : hourRef).current?.focus();
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
      {minuteOnly ? (
        <>
          {showIcon && (
            <Clock className="pointer-events-none size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="pointer-events-none select-none text-muted-foreground">
            at&nbsp;:
          </span>
          <SegmentInput
            ref={minuteRef}
            seg="minutes"
            value={mm}
            onValueChange={setMinute}
            disabled={disabled}
            ariaLabel="Minute"
          />
        </>
      ) : (
        <>
          {showIcon && (
            <Clock className="pointer-events-none size-3.5 shrink-0 text-muted-foreground" />
          )}
          <SegmentInput
            ref={hourRef}
            seg="hours"
            value={hh}
            onValueChange={setHour}
            onRightFocus={() => minuteRef.current?.focus()}
            disabled={disabled}
            ariaLabel="Hour"
          />
          <span className="pointer-events-none select-none text-muted-foreground">
            :
          </span>
          <SegmentInput
            ref={minuteRef}
            seg="minutes"
            value={mm}
            onValueChange={setMinute}
            onLeftFocus={() => hourRef.current?.focus()}
            disabled={disabled}
            ariaLabel="Minute"
          />
        </>
      )}
    </div>
  );
}
