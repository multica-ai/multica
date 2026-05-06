"use client";

import { useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { CalendarDays } from "lucide-react";
import { Views, type View } from "react-big-calendar";
import { BigCalendar } from "@/components/ui/big-calendar";
import type { Issue, IssueStatus } from "@/shared/types";
import { useIssuesListQuery } from "@/features/issues/queries";

// ── Colour mapping ─────────────────────────────────────────────────────────────

/** Maps issue status to an inline background colour for calendar events. */
const STATUS_COLORS: Record<IssueStatus, string> = {
  backlog: "var(--muted-foreground)",
  todo: "var(--muted-foreground)",
  in_progress: "var(--primary)",
  in_review: "#8b5cf6",       // violet
  done: "#22c55e",            // green
  blocked: "var(--destructive)",
  cancelled: "var(--muted-foreground)",
};

// ── Types ─────────────────────────────────────────────────────────────────────

interface IssueCalendarEvent {
  id: string;
  title: string;
  start: Date;
  end: Date;
  allDay: boolean;
  resource: Issue;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Converts an Issue to a calendar event.
 * Only issues with at least a start_date or end_date are included.
 * If only one side is set, the event spans that single day.
 */
function issueToEvent(issue: Issue): IssueCalendarEvent | null {
  const start = issue.start_date ? new Date(issue.start_date) : null;
  const end = issue.end_date ? new Date(issue.end_date) : null;

  // Skip issues with no date at all
  if (!start && !end) return null;

  const eventStart = start ?? end!;
  // end is inclusive — add 1 day so react-big-calendar renders it on the correct last day
  const eventEnd = end
    ? new Date(end.getTime() + 86400000)
    : new Date(eventStart.getTime() + 86400000);

  return {
    id: issue.id,
    title: `${issue.identifier} ${issue.title}`,
    start: eventStart,
    end: eventEnd,
    allDay: true,
    resource: issue,
  };
}

// ── Page ──────────────────────────────────────────────────────────────────────

/**
 * /calendar — issue scheduling calendar, month view by default.
 * Shows all issues that have a start_date or end_date as all-day events.
 * Clicking an event navigates to the issue detail page.
 */
export function IssueCalendarPage() {
  const navigate = useNavigate();
  const { data } = useIssuesListQuery({ limit: 500 });
  const issues = data?.issues ?? [];

  const [view, setView] = useState<View>(Views.MONTH);
  const [date, setDate] = useState(new Date());

  const events = useMemo(
    () => issues.flatMap((issue) => {
      const event = issueToEvent(issue);
      return event ? [event] : [];
    }),
    [issues],
  );

  const handleSelectEvent = (event: IssueCalendarEvent) => {
    navigate({ to: "/issues/$id", params: { id: event.resource.id } });
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-6 py-4">
        <CalendarDays className="size-5 text-muted-foreground" />
        <h1 className="text-lg font-semibold">Calendar</h1>
      </div>

      {/* Calendar */}
      <div className="flex-1 overflow-auto p-4">
        <BigCalendar<IssueCalendarEvent>
          events={events}
          view={view}
          onView={setView}
          date={date}
          onNavigate={setDate}
          defaultView={Views.MONTH}
          views={[Views.MONTH, Views.WEEK, Views.DAY, Views.AGENDA]}
          onSelectEvent={handleSelectEvent}
          style={{ height: "100%", minHeight: 600 }}
          // Colour events by issue status
          eventPropGetter={(event) => ({
            style: {
              backgroundColor: STATUS_COLORS[event.resource.status] ?? "var(--primary)",
              border: "none",
            },
          })}
        />
      </div>
    </div>
  );
}
