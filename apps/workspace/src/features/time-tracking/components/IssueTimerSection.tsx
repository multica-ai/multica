"use client";

import { useState } from "react";
import { Play, Trash2, Clock } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import type { TimeEntry } from "@/shared/types";
import {
  useCurrentTimerQuery,
  useIssueTimeEntriesQuery,
  useStartTimerMutation,
  useStopTimerMutation,
  useDeleteTimeEntryMutation,
} from "../hooks/use-time-tracking";
import { LiveDuration, formatDuration } from "./LiveDuration";

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Sums up all durations, including the live elapsed time for a running entry. */
function computeTotalSeconds(entries: TimeEntry[]): number {
  return entries.reduce((sum, e) => {
    if (e.duration_seconds < 0) {
      // Running entry: compute current elapsed from Toggl convention.
      return sum + Math.max(0, Math.floor(Date.now() / 1000) + e.duration_seconds);
    }
    return sum + e.duration_seconds;
  }, 0);
}

/** Formats an ISO date string as a short locale date (e.g. "Jun 10"). */
function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

// ── TimeEntryRow ─────────────────────────────────────────────────────────────

interface TimeEntryRowProps {
  entry: TimeEntry;
  isRunning: boolean;
  onDelete: () => void;
}

function TimeEntryRow({ entry, isRunning, onDelete }: TimeEntryRowProps) {
  return (
    <div className="flex items-center gap-2 py-1 text-sm group">
      <div className="min-w-0 flex-1">
        {entry.description ? (
          <span className="truncate text-foreground">{entry.description}</span>
        ) : (
          <span className="text-muted-foreground italic">No description</span>
        )}
        <span className="ml-2 text-xs text-muted-foreground">{shortDate(entry.start_time)}</span>
      </div>
      <div className="shrink-0">
        {isRunning ? (
          <LiveDuration entry={entry} className="text-sm font-mono text-brand" />
        ) : (
          <span className="font-mono text-xs text-muted-foreground">
            {formatDuration(entry.duration_seconds)}
          </span>
        )}
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              size="icon"
              variant="ghost"
              className="size-6 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity"
              aria-label="Entry options"
            />
          }
        >
          <span className="sr-only">Options</span>
          <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor" aria-hidden>
            <circle cx="6" cy="2" r="1.2" />
            <circle cx="6" cy="6" r="1.2" />
            <circle cx="6" cy="10" r="1.2" />
          </svg>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            className="text-destructive focus:text-destructive"
            onClick={onDelete}
          >
            <Trash2 className="mr-2 size-4" />
            Delete entry
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

// ── IssueTimerSection ─────────────────────────────────────────────────────────

interface IssueTimerSectionProps {
  issueId: string;
}

/**
 * Displays the time tracking section inside an issue detail panel.
 *
 * Shows:
 * - Total time badge (live if a timer is running for this issue)
 * - "Start tracking" / "Stop" button wired to the global timer
 * - List of all past time entries for this issue
 */
export function IssueTimerSection({ issueId }: IssueTimerSectionProps) {
  const [showAll, setShowAll] = useState(false);

  const { data: entriesData, isLoading } = useIssueTimeEntriesQuery(issueId);
  const { data: currentEntry } = useCurrentTimerQuery();
  const startMutation = useStartTimerMutation();
  const stopMutation = useStopTimerMutation();
  const deleteMutation = useDeleteTimeEntryMutation();

  // entriesData is TimeEntry[] (API returns array directly)
  const entries: TimeEntry[] = entriesData ?? [];

  // Check if the current running timer belongs to this issue.
  const isTrackingThisIssue = currentEntry?.issue_id === issueId;
  const isAnotherIssueRunning = !!currentEntry && !isTrackingThisIssue;

  // Combine issue entries with the running entry for display (avoid duplication).
  const allEntries: TimeEntry[] = isTrackingThisIssue && currentEntry
    ? entries.some((e) => e.id === currentEntry.id)
      ? entries
      : [currentEntry, ...entries]
    : entries;

  const DISPLAY_LIMIT = 5;
  const displayedEntries = showAll ? allEntries : allEntries.slice(0, DISPLAY_LIMIT);
  const totalSeconds = computeTotalSeconds(allEntries);

  const handleStart = () => {
    const now = new Date().toISOString();
    startMutation.mutate(
      { issue_id: issueId, start_time: now },
      { onError: () => toast.error("Failed to start timer") },
    );
  };

  const handleStop = () => {
    if (!currentEntry) return;
    stopMutation.mutate(currentEntry.id, {
      onError: () => toast.error("Failed to stop timer"),
    });
  };

  const handleDelete = (entry: TimeEntry) => {
    deleteMutation.mutate(
      { id: entry.id, issueId },
      { onError: () => toast.error("Failed to delete entry") },
    );
  };

  return (
    <div>
      {/* Section header */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Clock className="size-4 text-muted-foreground" />
          <h3 className="text-sm font-semibold">Time</h3>
          {totalSeconds > 0 && (
            <Badge variant="secondary" className="text-xs font-mono">
              {formatDuration(totalSeconds)}
            </Badge>
          )}
        </div>

        {/* Start / Stop button */}
        {isTrackingThisIssue ? (
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1.5 text-xs border-destructive text-destructive hover:bg-destructive/10"
            disabled={stopMutation.isPending}
            onClick={handleStop}
          >
            <span className="inline-block size-2 rounded-sm bg-destructive" />
            Stop
          </Button>
        ) : (
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1.5 text-xs"
            disabled={startMutation.isPending || isAnotherIssueRunning}
            title={isAnotherIssueRunning ? "Another timer is already running" : undefined}
            onClick={handleStart}
          >
            <Play className="size-3 fill-current" />
            {isAnotherIssueRunning ? "Timer running" : "Start"}
          </Button>
        )}
      </div>

      {/* Entry list */}
      {isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-5 w-full" />
          <Skeleton className="h-5 w-3/4" />
        </div>
      ) : allEntries.length === 0 ? (
        <p className="text-xs text-muted-foreground">No time logged yet.</p>
      ) : (
        <div className="divide-y divide-border/50">
          {displayedEntries.map((entry) => (
            <TimeEntryRow
              key={entry.id}
              entry={entry}
              isRunning={entry.id === currentEntry?.id}
              onDelete={() => handleDelete(entry)}
            />
          ))}
          {allEntries.length > DISPLAY_LIMIT && (
            <button
              className="pt-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
              onClick={() => setShowAll((v) => !v)}
            >
              {showAll
                ? "Show less"
                : `Show ${allEntries.length - DISPLAY_LIMIT} more entries`}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
