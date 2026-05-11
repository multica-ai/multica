import { useEffect, useState } from "react";
import type { TimeEntry } from "@/shared/types";
import { cn } from "@/lib/utils";

/**
 * Formats a duration in seconds as "h:mm:ss" or "m:ss" (no leading zeros on hours/minutes).
 */
export function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h > 0) {
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  }
  return `${m}:${String(s).padStart(2, "0")}`;
}

/**
 * Computes elapsed seconds from a TimeEntry.
 * - Running entries (duration_seconds < 0) use Toggl convention:
 *   elapsed = Math.floor(Date.now() / 1000) + duration_seconds
 *   (duration_seconds is stored as -start_time.Unix())
 * - Stopped entries just return the stored duration_seconds.
 */
export function getElapsedSeconds(entry: TimeEntry): number {
  if (entry.duration_seconds < 0) {
    // Live timer: derive elapsed from the negative unix-timestamp convention.
    return Math.max(0, Math.floor(Date.now() / 1000) + entry.duration_seconds);
  }
  return entry.duration_seconds;
}

interface LiveDurationProps {
  /** The time entry to display duration for. */
  entry: TimeEntry;
  className?: string;
}

/**
 * Displays a live-ticking duration for a running time entry.
 *
 * Critical: initializes from entry.start_time so there is no flash (counter
 * doesn't jump from 0:00 to the real elapsed time on first render).
 *
 * For stopped entries it simply renders the stored duration without ticking.
 */
export function LiveDuration({ entry, className }: LiveDurationProps) {
  // Initialize directly from elapsed time — no 0 flash on mount.
  const [seconds, setSeconds] = useState(() => getElapsedSeconds(entry));
  const isRunning = entry.duration_seconds < 0;

  useEffect(() => {
    // Sync whenever the entry itself changes (e.g. WS update).
    setSeconds(getElapsedSeconds(entry));

    if (!isRunning) return;

    const interval = setInterval(() => {
      setSeconds(getElapsedSeconds(entry));
    }, 1000);

    return () => clearInterval(interval);
  }, [entry, isRunning]);

  return <span className={cn("font-mono tabular-nums", className)}>{formatDuration(seconds)}</span>;
}
