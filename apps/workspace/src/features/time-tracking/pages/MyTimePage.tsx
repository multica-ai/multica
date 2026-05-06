"use client";

import { useMemo, useState, useRef } from "react";
import { Link } from "@tanstack/react-router";
import { Clock, Play, Square, Pencil, CalendarDays } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import type { TimeEntry } from "@/shared/types";
import {
  useCurrentTimerQuery,
  useTimeEntriesQuery,
  useStartTimerMutation,
  useStopTimerMutation,
} from "../hooks/use-time-tracking";
import { LiveDuration, formatDuration } from "../components/LiveDuration";
import { TimeEntryEditSheet } from "../components/TimeEntryEditSheet";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Groups time entries by calendar date (YYYY-MM-DD). */
function groupByDay(entries: TimeEntry[]): Map<string, TimeEntry[]> {
  const map = new Map<string, TimeEntry[]>();
  for (const entry of entries) {
    const key = entry.start_time.slice(0, 10);
    const bucket = map.get(key) ?? [];
    bucket.push(entry);
    map.set(key, bucket);
  }
  return map;
}

/** Formats a date string (YYYY-MM-DD) as "Today", "Yesterday", or "Jun 10". */
function formatDayLabel(dateStr: string): string {
  const today = new Date().toISOString().slice(0, 10);
  const yesterday = new Date(Date.now() - 86400000).toISOString().slice(0, 10);
  if (dateStr === today) return "Today";
  if (dateStr === yesterday) return "Yesterday";
  return new Date(dateStr).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/** Sums all durations in a day bucket. */
function sumDuration(entries: TimeEntry[]): number {
  return entries.reduce((acc, e) => {
    if (e.duration_seconds < 0) {
      return acc + Math.max(0, Math.floor(Date.now() / 1000) + e.duration_seconds);
    }
    return acc + e.duration_seconds;
  }, 0);
}

// ── Start timer bar ───────────────────────────────────────────────────────────

/**
 * Inline bar at the top of My Time page that lets users kick off a new timer.
 * Hidden while a timer is already running.
 */
function StartTimerBar() {
  const [description, setDescription] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const startMutation = useStartTimerMutation();

  const handleStart = () => {
    const now = new Date().toISOString();
    startMutation.mutate(
      { description: description.trim() || undefined, start_time: now },
      {
        onSuccess: () => setDescription(""),
        onError: () => toast.error("Failed to start timer"),
      },
    );
  };

  return (
    <div className="flex items-center gap-2 rounded-lg border bg-card px-4 py-3">
      <Play className="size-4 shrink-0 text-muted-foreground" />
      <Input
        ref={inputRef}
        placeholder="What are you working on? (press Enter to start)"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") handleStart();
        }}
        className="h-8 flex-1 border-0 bg-transparent p-0 text-sm shadow-none focus-visible:ring-0"
      />
      <Button
        size="sm"
        className="shrink-0"
        disabled={startMutation.isPending}
        onClick={handleStart}
      >
        <Play className="mr-1.5 size-3.5" />
        Start
      </Button>
    </div>
  );
}



function RunningTimerCard({ entry }: { entry: TimeEntry }) {
  const stopMutation = useStopTimerMutation();

  const handleStop = () => {
    stopMutation.mutate(entry.id, {
      onError: () => toast.error("Failed to stop timer"),
    });
  };

  return (
    <div className="flex items-center gap-3 rounded-lg border border-brand/40 bg-brand/5 px-4 py-3">
      <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-brand/20">
        <Clock className="size-4 text-brand" />
      </div>
      <div className="min-w-0 flex-1">
        {entry.description ? (
          <p className="truncate text-sm font-medium">{entry.description}</p>
        ) : (
          <p className="text-sm text-muted-foreground italic">No description</p>
        )}
        <p className="text-xs text-muted-foreground">Running now</p>
      </div>
      <LiveDuration entry={entry} className="shrink-0 text-base font-semibold text-brand" />
      <Button
        size="icon"
        variant="outline"
        className="size-8 shrink-0 border-destructive text-destructive hover:bg-destructive/10"
        disabled={stopMutation.isPending}
        onClick={handleStop}
        aria-label="Stop timer"
      >
        <Square className="size-3.5 fill-current" />
      </Button>
    </div>
  );
}

// ── Entry row ─────────────────────────────────────────────────────────────────

function EntryRow({
  entry,
  isRunning,
  onEdit,
}: {
  entry: TimeEntry;
  isRunning: boolean;
  onEdit: (entry: TimeEntry) => void;
}) {
  return (
    <button
      type="button"
      className="flex w-full items-center gap-3 py-2 text-sm text-left group hover:bg-muted/40 transition-colors rounded px-1 -mx-1"
      onClick={() => onEdit(entry)}
      aria-label="Edit time entry"
    >
      <div className="min-w-0 flex-1">
        {entry.description ? (
          <span className="text-foreground">{entry.description}</span>
        ) : (
          <span className="text-muted-foreground italic">No description</span>
        )}
      </div>
      {isRunning ? (
        <LiveDuration entry={entry} className="shrink-0 font-mono text-sm text-brand" />
      ) : (
        <span className="shrink-0 font-mono text-xs text-muted-foreground">
          {formatDuration(entry.duration_seconds)}
        </span>
      )}
      <Pencil className="size-3.5 shrink-0 opacity-0 group-hover:opacity-60 transition-opacity text-muted-foreground" />
    </button>
  );
}

// ── MyTimePage ────────────────────────────────────────────────────────────────

/**
 * Full-page view for the current user's time tracking history.
 *
 * Sections:
 * - Running timer card (if active)
 * - Time entries grouped by day with a daily total
 */
export function MyTimePage() {
  const { data: currentEntry } = useCurrentTimerQuery();
  const { data: listData, isLoading } = useTimeEntriesQuery({ limit: 200 });

  // Controls which entry is open in the edit sheet.
  const [editingEntry, setEditingEntry] = useState<TimeEntry | null>(null);

  // API returns TimeEntry[] directly
  const entries: TimeEntry[] = listData ?? [];

  // Sort entries newest-first and group by day.
  const grouped = useMemo(() => {
    const sorted = [...entries].sort(
      (a, b) => new Date(b.start_time).getTime() - new Date(a.start_time).getTime(),
    );
    return groupByDay(sorted);
  }, [entries]);

  const days = Array.from(grouped.keys());

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Page header */}
      <div className="flex items-center justify-between border-b px-6 py-4">
        <div className="flex items-center gap-2">
          <Clock className="size-5 text-muted-foreground" />
          <h1 className="text-lg font-semibold">My Time</h1>
        </div>
        <Link to="/my-time/calendar" className={buttonVariants({ variant: "outline", size: "sm" })}>
            <CalendarDays className="mr-1.5 size-3.5" />
            Calendar
          </Link>
      </div>

      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto max-w-2xl space-y-6">
          {/* Running timer card or start timer bar */}
          {currentEntry ? (
            <RunningTimerCard entry={currentEntry} />
          ) : (
            <StartTimerBar />
          )}

          {/* Entry history */}
          {isLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-6 w-24" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : days.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-16 text-center">
              <Clock className="size-10 text-muted-foreground/40" />
              <p className="text-sm text-muted-foreground">No time entries yet.</p>
              <p className="text-xs text-muted-foreground">
                Use the bar above to start tracking your first timer.
              </p>
            </div>
          ) : (
            days.map((day) => {
              const dayEntries = grouped.get(day) ?? [];
              const dayTotal = sumDuration(dayEntries);
              return (
                <div key={day}>
                  {/* Day header */}
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-sm font-medium">{formatDayLabel(day)}</span>
                    <Badge variant="secondary" className="font-mono text-xs">
                      {formatDuration(dayTotal)}
                    </Badge>
                  </div>
                  {/* Entries for this day */}
                  <div className="rounded-lg border divide-y">
                    {dayEntries.map((entry) => (
                      <div key={entry.id} className="px-4">
                        <EntryRow
                          entry={entry}
                          isRunning={entry.id === currentEntry?.id}
                          onEdit={setEditingEntry}
                        />
                      </div>
                    ))}
                  </div>
                </div>
              );
            })
          )}
        </div>
      </div>

      {/* Time entry edit sheet */}
      <TimeEntryEditSheet
        entry={editingEntry}
        onClose={() => setEditingEntry(null)}
      />
    </div>
  );
}
