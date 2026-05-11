"use client";

import { useMemo } from "react";
import { Timer } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import type { TimeEntry } from "@/shared/types";
import { usePomodoroHistoryQuery } from "../hooks/use-pomodoro-history";

// ── Utilities ─────────────────────────────────────────────────────────────────

/** Formats seconds to "Xh Ym" or "Ym" strings. */
function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

/** Returns "YYYY-MM-DD" in local time for the given ISO timestamp. */
function localDateKey(isoString: string): string {
  const d = new Date(isoString);
  const y = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${mo}-${day}`;
}

/** Formats "YYYY-MM-DD" as "Today", "Yesterday", or "Jun 10". */
function formatDayLabel(dateStr: string): string {
  const today = localDateKey(new Date().toISOString());
  const yesterday = localDateKey(new Date(Date.now() - 86_400_000).toISOString());
  if (dateStr === today) return "Today";
  if (dateStr === yesterday) return "Yesterday";
  return new Date(`${dateStr}T12:00:00`).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

/** Groups time entries by local calendar date (YYYY-MM-DD), sorted newest first. */
function groupByDay(entries: TimeEntry[]): [string, TimeEntry[]][] {
  const map = new Map<string, TimeEntry[]>();
  for (const entry of entries) {
    const key = localDateKey(entry.start_time);
    const bucket = map.get(key) ?? [];
    bucket.push(entry);
    map.set(key, bucket);
  }
  return [...map.entries()].sort((a, b) => b[0].localeCompare(a[0]));
}

/**
 * Computes the current consecutive-day streak.
 * Counts backwards from today; stops at the first day with no pomodoro.
 */
function computeStreak(entries: TimeEntry[]): number {
  if (entries.length === 0) return 0;
  const days = new Set(entries.map((e) => localDateKey(e.start_time)));
  let streak = 0;
  const now = new Date();
  for (let i = 0; i < 365; i++) {
    const d = new Date(now);
    d.setDate(d.getDate() - i);
    if (days.has(localDateKey(d.toISOString()))) {
      streak++;
    } else {
      break;
    }
  }
  return streak;
}

/** Formats a UTC ISO timestamp as local "HH:MM". */
function formatTime(isoString: string): string {
  return new Date(isoString).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

// ── Sub-components ─────────────────────────────────────────────────────────────

interface StatCardProps {
  label: string;
  value: string | number;
}

/** Simple stat card used in the summary row. */
function StatCard({ label, value }: StatCardProps) {
  return (
    <div className="rounded-lg border bg-card px-4 py-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
    </div>
  );
}

/** Single pomodoro history entry row. */
function PomodoroEntryRow({ entry }: { entry: TimeEntry }) {
  const durationSec =
    entry.duration_seconds > 0
      ? entry.duration_seconds
      : Math.round(Date.now() / 1000 + entry.duration_seconds);

  return (
    <div className="flex items-center gap-3 py-2 text-sm border-b last:border-0">
      <span className="text-base shrink-0">🍅</span>
      <span className="font-medium tabular-nums shrink-0">{formatDuration(durationSec)}</span>
      <span className="text-muted-foreground shrink-0">{formatTime(entry.start_time)}</span>
      {entry.description && (
        <span className="truncate text-muted-foreground">{entry.description}</span>
      )}
      {entry.issue_id && (
        <span className="ml-auto shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs font-mono text-muted-foreground">
          #{entry.issue_id.slice(0, 8)}
        </span>
      )}
    </div>
  );
}

// ── PomodoroPage ───────────────────────────────────────────────────────────────

/**
 * Pomodoro history and stats page.
 * Displays aggregate stats cards and a chronological history grouped by day.
 */
export function PomodoroPage() {
  const { data, isLoading } = usePomodoroHistoryQuery();

  const entries = data?.entries ?? [];
  const stats = data?.stats;

  // Compute consecutive-day streak client-side.
  const streak = useMemo(() => computeStreak(entries), [entries]);

  // Group entries by local date for the history section.
  const grouped = useMemo(() => groupByDay(entries), [entries]);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Page header */}
      <div className="flex items-center gap-2 border-b px-6 py-4">
        <Timer className="size-5 text-muted-foreground" />
        <h1 className="text-lg font-semibold">Pomodoro</h1>
      </div>

      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto max-w-2xl space-y-8">

          {/* Stats cards */}
          {isLoading ? (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-20 w-full rounded-lg" />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <StatCard label="Today" value={stats?.today_count ?? 0} />
              <StatCard label="This Week" value={stats?.week_count ?? 0} />
              <StatCard
                label="Total Focus"
                value={stats ? formatDuration(stats.total_seconds) : "0m"}
              />
              <StatCard label="Streak" value={`${streak}d`} />
            </div>
          )}

          {/* History */}
          {isLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-5 w-24" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : entries.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <span className="text-4xl mb-3">🍅</span>
              <p className="font-medium">No pomodoro sessions yet.</p>
              <p className="mt-1 text-sm text-muted-foreground">
                Start your first one from the timer widget!
              </p>
            </div>
          ) : (
            <section className="space-y-6">
              <h2 className="text-sm font-semibold">History</h2>
              {grouped.map(([dateKey, dayEntries]) => (
                <div key={dateKey}>
                  <p className="mb-2 text-xs font-medium text-muted-foreground uppercase tracking-wide">
                    {formatDayLabel(dateKey)}
                  </p>
                  <div className="rounded-lg border px-4">
                    {dayEntries.map((entry) => (
                      <PomodoroEntryRow key={entry.id} entry={entry} />
                    ))}
                  </div>
                </div>
              ))}
            </section>
          )}

        </div>
      </div>
    </div>
  );
}
