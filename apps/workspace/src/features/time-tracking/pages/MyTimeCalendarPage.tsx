"use client";

import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Clock, List } from "lucide-react";
import { Views, type View } from "react-big-calendar";
import { buttonVariants } from "@/components/ui/button";
import { BigCalendar } from "@/components/ui/big-calendar";
import type { TimeEntry } from "@/shared/types";
import {
  useCurrentTimerQuery,
  useTimeEntriesQuery,
} from "../hooks/use-time-tracking";
import { TimeEntryEditSheet } from "../components/TimeEntryEditSheet";

// ── Types ─────────────────────────────────────────────────────────────────────

/** Calendar event shape for a time entry. */
interface TimeEntryEvent {
  id: string;
  title: string;
  start: Date;
  end: Date;
  resource: TimeEntry;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Converts a TimeEntry to a react-big-calendar event.
 * Running entries end at "now" so they appear on the calendar.
 */
function entryToEvent(entry: TimeEntry): TimeEntryEvent {
  const start = new Date(entry.start_time);
  const end = entry.stop_time ? new Date(entry.stop_time) : new Date();
  const title = entry.description?.trim() || "Time entry";
  return { id: entry.id, title, start, end, resource: entry };
}

// ── Page ──────────────────────────────────────────────────────────────────────

/**
 * /my-time/calendar — time entries displayed as a react-big-calendar day view.
 * Clicking an event opens the TimeEntryEditSheet for editing.
 */
export function MyTimeCalendarPage() {
  const { data: entries = [] } = useTimeEntriesQuery();
  const { data: running } = useCurrentTimerQuery();

  const [view, setView] = useState<View>(Views.DAY);
  const [date, setDate] = useState(new Date());
  const [editingEntry, setEditingEntry] = useState<TimeEntry | null>(null);

  // Merge running timer (if any) with historical entries, deduplicate by id.
  const allEntries = useMemo<TimeEntry[]>(() => {
    if (!running) return entries;
    const seen = new Set(entries.map((e) => e.id));
    return seen.has(running.id) ? entries : [running, ...entries];
  }, [entries, running]);

  const events = useMemo(() => allEntries.map(entryToEvent), [allEntries]);

  const handleSelectEvent = (event: TimeEntryEvent) => {
    setEditingEntry(event.resource);
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-6 py-4">
        <div className="flex items-center gap-2">
          <Clock className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">My Time — Calendar</h1>
        </div>
        <Link to="/my-time" className={buttonVariants({ variant: "outline", size: "sm" })}>
            <List className="mr-1.5 size-3.5" />
            List view
          </Link>
      </div>

      {/* Calendar */}
      <div className="flex-1 overflow-auto p-4">
        <BigCalendar<TimeEntryEvent>
          events={events}
          view={view}
          onView={setView}
          date={date}
          onNavigate={setDate}
          defaultView={Views.DAY}
          views={[Views.DAY, Views.WEEK, Views.MONTH, Views.AGENDA]}
          onSelectEvent={handleSelectEvent}
          style={{ height: "100%", minHeight: 600 }}
          // Show start time label on each event
          eventPropGetter={(event) => ({
            style: {
              // Running timers shown with a pulsing primary/50 background
              opacity: event.resource.stop_time === null ? 0.75 : 1,
            },
          })}
        />
      </div>

      {/* Edit sheet */}
      <TimeEntryEditSheet
        entry={editingEntry}
        onClose={() => setEditingEntry(null)}
      />
    </div>
  );
}
