// CEREBRO: FIR-1521 (part 2 polish) — inbox row indicator for a scheduled wakeup.
// The previous inbox marker was a static grey clock with no countdown, so a user
// couldn't tell it was live (Jesper feedback). This renders an orange alarm clock
// with a ticking countdown (for time triggers), matching the top-of-issue banner
// and the list/board pip. For condition triggers (status / CI) there is no fire
// time, so it shows just the orange clock with the label as a tooltip.
"use client";

import { useEffect, useState } from "react";
import { AlarmClock } from "lucide-react";
import { formatCountdown } from "../lib/wakeup-format";

export function CerebroInboxWakeupPip({
  fireAt,
  title,
  className = "",
}: {
  /** RFC3339 fire time for a time trigger; absent for status / CI triggers. */
  fireAt?: string;
  /** Tooltip / fallback label (e.g. "Planlagt: kører ved statusskift"). */
  title?: string;
  className?: string;
}) {
  const [now, setNow] = useState(() => Date.now());

  // Tick once a second so the countdown is visibly live. Only time triggers have
  // a countdown, so condition triggers never start a timer.
  useEffect(() => {
    if (!fireAt) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [fireAt]);

  const countdown = fireAt ? formatCountdown(fireAt, now) : undefined;

  return (
    <span
      className={`inline-flex shrink-0 items-center gap-0.5 text-warning ${className}`}
      title={title}
    >
      <AlarmClock className="size-3 shrink-0" />
      {countdown && (
        <span className="text-[10px] leading-none tabular-nums">{countdown}</span>
      )}
    </span>
  );
}
