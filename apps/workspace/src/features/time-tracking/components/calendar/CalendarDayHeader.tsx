/**
 * CalendarDayHeader — memo-wrapped day header for react-big-calendar's `components.header` slot.
 *
 * Displays the date number, weekday abbreviation, and daily total duration.
 * Today's date number gets a primary-color background circle.
 *
 * Because RBC's `components.header` only receives `{ date, label, localizer }`, we
 * use a factory function to close over `dailyTotals` and `today`.
 */
import React, { memo } from "react";
import { formatDuration } from "../LiveDuration";

// ── Day header component ──────────────────────────────────────────────────────

interface CalendarDayHeaderProps {
  date: Date;
  dailyTotals: Map<string, number>;
  today: Date;
}

function CalendarDayHeaderImpl({ date, dailyTotals, today }: CalendarDayHeaderProps) {
  const dayNum = date.getDate();

  // Three-letter weekday abbreviation, uppercase (e.g. "MON")
  const dayName = new Intl.DateTimeFormat("en-US", { weekday: "short" })
    .format(date)
    .toUpperCase();

  // Key used to look up totals in dailyTotals map: "YYYY-MM-DD"
  const dateKey = new Intl.DateTimeFormat("en-CA", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);

  const totalSeconds = dailyTotals.get(dateKey) ?? 0;

  const isToday =
    date.getFullYear() === today.getFullYear() &&
    date.getMonth() === today.getMonth() &&
    date.getDate() === today.getDate();

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: "8px",
        padding: "8px",
        width: "100%",
      }}
    >
      {/* Date number with optional today highlight */}
      <span
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          width: "32px",
          height: "32px",
          fontSize: "1.25rem",
          fontWeight: 600,
          lineHeight: 1,
          borderRadius: isToday ? "50%" : undefined,
          backgroundColor: isToday ? "color-mix(in srgb, var(--primary) 20%, transparent)" : undefined,
          color: isToday ? "var(--primary)" : "var(--foreground)",
          flexShrink: 0,
        }}
      >
        {dayNum}
      </span>

      {/* Weekday + daily total */}
      <span style={{ display: "flex", flexDirection: "column", alignItems: "flex-start", lineHeight: 1.3 }}>
        <span
          style={{
            fontSize: "0.6875rem",
            fontWeight: 500,
            letterSpacing: "0.05em",
            color: isToday ? "var(--primary)" : "var(--muted-foreground)",
          }}
        >
          {dayName}
        </span>
        <span
          style={{
            fontSize: "0.6875rem",
            fontVariantNumeric: "tabular-nums",
            color: "var(--muted-foreground)",
          }}
        >
          {totalSeconds > 0 ? formatDuration(totalSeconds) : "0:00:00"}
        </span>
      </span>
    </div>
  );
}

const CalendarDayHeaderMemo = memo(CalendarDayHeaderImpl);

// ── Factory ───────────────────────────────────────────────────────────────────

/**
 * Returns a memoized header component that closes over `dailyTotals` and `today`.
 *
 * Call this inside a `useMemo` in the parent so the component reference is stable
 * and RBC doesn't unmount/remount the header on every render.
 */
export function createDayHeaderComponent(dailyTotals: Map<string, number>, today: Date) {
  // RBC passes { date, label, localizer } — we only need `date`.
  function DayHeader({ date }: { date: Date; label?: string }) {
    return <CalendarDayHeaderMemo date={date} dailyTotals={dailyTotals} today={today} />;
  }
  return memo(DayHeader);
}
