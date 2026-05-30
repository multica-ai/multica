"use client";

import { useEffect, useMemo, useRef } from "react";
import { cn } from "@multica/ui/lib/utils";

/**
 * Slack-style scroll-wheel date/time picker for the mobile reminder sheet
 * (FIR-2526). Three independent scroll-snap columns — day, hour, minute —
 * give the iOS-native "wheel" feel on touch without pulling in a heavy
 * picker dependency: CSS `scroll-snap` centres the rows, and we read back
 * the settled index once scrolling stops.
 *
 * Desktop keeps the Calendar + TimeInput surface instead — a momentum wheel
 * is a poor fit for a mouse — so this component is only mounted on mobile.
 */

/** Pixel height of one wheel row. Must match the inline row height below. */
const ROW_H = 36;
/** Odd number of rows visible at once; the centre row is the selection. */
const VISIBLE_ROWS = 5;
/** Top/bottom spacer so the first and last rows can scroll to the centre. */
const PAD = ((VISIBLE_ROWS - 1) / 2) * ROW_H;
/** How many days forward the day column offers. */
const DAY_SPAN = 90;

export interface DayOption {
  /** Midnight (local) of the day this row represents. */
  date: Date;
  label: string;
}

/**
 * Build the day column: one row per day from `from`'s day for `count` days.
 * The first two days render as the localized "Today" / "Tomorrow" labels;
 * the rest render as a short weekday + day + month string. Pure + exported
 * so the index↔date mapping can be unit-tested without a DOM.
 */
export function buildDayOptions(
  from: Date,
  count: number,
  todayLabel: string,
  tomorrowLabel: string,
  locale?: string,
): DayOption[] {
  const start = new Date(from);
  start.setHours(0, 0, 0, 0);
  const fmt = new Intl.DateTimeFormat(locale, {
    weekday: "short",
    day: "numeric",
    month: "short",
  });
  const options: DayOption[] = [];
  for (let i = 0; i < count; i++) {
    const date = new Date(start);
    date.setDate(start.getDate() + i);
    const label = i === 0 ? todayLabel : i === 1 ? tomorrowLabel : fmt.format(date);
    options.push({ date, label });
  }
  return options;
}

/** Clamp an index into `[0, length - 1]`. */
function clampIndex(index: number, length: number): number {
  return Math.max(0, Math.min(length - 1, index));
}

const HOURS = Array.from({ length: 24 }, (_, i) => i);
const MINUTES = Array.from({ length: 60 }, (_, i) => i);
const pad2 = (n: number) => String(n).padStart(2, "0");

interface Props {
  /** Currently selected moment. */
  value: Date;
  /** Localized labels for the first two day rows. */
  todayLabel: string;
  tomorrowLabel: string;
  /** Optional BCP-47 locale for the weekday/month formatting. */
  locale?: string;
  onChange: (next: Date) => void;
}

export function CerebroReminderWheel({
  value,
  todayLabel,
  tomorrowLabel,
  locale,
  onChange,
}: Props) {
  const days = useMemo(
    () => buildDayOptions(value, DAY_SPAN, todayLabel, tomorrowLabel, locale),
    [value, todayLabel, tomorrowLabel, locale],
  );

  // Index of the selected value within each column. The day index is the
  // whole-day delta between `value` and today; clamped so an out-of-range
  // value (shouldn't happen) still lands on a real row.
  const dayIndex = useMemo(() => {
    const start = new Date(days[0]!.date);
    const target = new Date(value);
    target.setHours(0, 0, 0, 0);
    const delta = Math.round(
      (target.getTime() - start.getTime()) / (24 * 60 * 60 * 1000),
    );
    return clampIndex(delta, days.length);
  }, [days, value]);

  const setFromIndexes = (di: number, hi: number, mi: number) => {
    const day = days[clampIndex(di, days.length)]!.date;
    const next = new Date(day);
    next.setHours(clampIndex(hi, 24), clampIndex(mi, 60), 0, 0);
    onChange(next);
  };

  return (
    <div
      className="relative select-none"
      style={{ height: VISIBLE_ROWS * ROW_H }}
      role="group"
      aria-label="Date and time picker"
    >
      {/* Centre selection band — purely decorative, never intercepts touch. */}
      <div
        className="pointer-events-none absolute inset-x-0 z-10 border-y border-border bg-accent/40"
        style={{ top: PAD, height: ROW_H }}
        aria-hidden
      />
      <div className="flex h-full">
        <WheelColumn
          ariaLabel="Day"
          className="flex-[2]"
          length={days.length}
          index={dayIndex}
          renderRow={(i) => days[i]!.label}
          onIndexChange={(i) => setFromIndexes(i, value.getHours(), value.getMinutes())}
        />
        <WheelColumn
          ariaLabel="Hour"
          length={HOURS.length}
          index={value.getHours()}
          renderRow={(i) => pad2(HOURS[i]!)}
          onIndexChange={(i) => setFromIndexes(dayIndex, i, value.getMinutes())}
        />
        <WheelColumn
          ariaLabel="Minute"
          length={MINUTES.length}
          index={value.getMinutes()}
          renderRow={(i) => pad2(MINUTES[i]!)}
          onIndexChange={(i) => setFromIndexes(dayIndex, value.getHours(), i)}
        />
      </div>
    </div>
  );
}

function WheelColumn({
  ariaLabel,
  className,
  length,
  index,
  renderRow,
  onIndexChange,
}: {
  ariaLabel: string;
  className?: string;
  length: number;
  index: number;
  renderRow: (i: number) => string;
  onIndexChange: (i: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const settleRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Suppress the scroll handler while we programmatically align the column to
  // an externally-changed index, so syncing doesn't echo back as a change.
  const syncingRef = useRef(false);

  // Align the column to `index` whenever it changes from the outside (initial
  // mount, or another column's edit recomputing this one's selection).
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const target = index * ROW_H;
    if (Math.abs(el.scrollTop - target) < 1) return;
    syncingRef.current = true;
    el.scrollTop = target;
    // Release the guard after the scroll settles on the next frame.
    const id = window.setTimeout(() => {
      syncingRef.current = false;
    }, 60);
    return () => window.clearTimeout(id);
  }, [index]);

  const handleScroll = () => {
    if (syncingRef.current) return;
    const el = ref.current;
    if (!el) return;
    if (settleRef.current !== null) clearTimeout(settleRef.current);
    settleRef.current = setTimeout(() => {
      const next = clampIndex(Math.round(el.scrollTop / ROW_H), length);
      if (next !== index) onIndexChange(next);
    }, 120);
  };

  useEffect(
    () => () => {
      if (settleRef.current !== null) clearTimeout(settleRef.current);
    },
    [],
  );

  return (
    <div
      ref={ref}
      onScroll={handleScroll}
      role="listbox"
      aria-label={ariaLabel}
      className={cn(
        "h-full snap-y snap-mandatory overflow-y-auto overscroll-contain text-center [&::-webkit-scrollbar]:hidden",
        className,
      )}
      style={{ scrollbarWidth: "none" }}
    >
      <div style={{ height: PAD }} aria-hidden />
      {Array.from({ length }, (_, i) => (
        <div
          key={i}
          role="option"
          aria-selected={i === index}
          className={cn(
            "flex snap-center items-center justify-center text-base tabular-nums transition-colors",
            i === index ? "font-medium text-foreground" : "text-muted-foreground/60",
          )}
          style={{ height: ROW_H }}
        >
          {renderRow(i)}
        </div>
      ))}
      <div style={{ height: PAD }} aria-hidden />
    </div>
  );
}
