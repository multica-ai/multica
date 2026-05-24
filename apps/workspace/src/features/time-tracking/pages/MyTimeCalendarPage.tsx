import { useMemo, useState, useCallback, useRef, useEffect } from "react";
import { Link } from "@tanstack/react-router";
import { Clock, List, Plus } from "lucide-react";
import { Views, type View } from "react-big-calendar";
import { toast } from "sonner";
import { Button, buttonVariants } from "@/components/ui/button";
import { BigDnDCalendar, type EventInteractionArgs } from "@/components/ui/big-calendar";
import type { TimeEntry } from "@/shared/types";
import {
  useCurrentTimerQuery,
  useTimeEntriesQuery,
  useUpdateTimeEntryMutation,
} from "../hooks/use-time-tracking";
import { useTimeEntryActions } from "../hooks/use-time-entry-actions";
import { TimeEntryEditSheet } from "../components/TimeEntryEditSheet";
import { TimeEntryCreateSheet } from "../components/TimeEntryCreateSheet";
import { TimeEntryDeleteDialog } from "../components/TimeEntryDeleteDialog";
import { ConfirmTimerSwitchDialog } from "../components/ConfirmTimerSwitchDialog";
import { CalendarEventCard, type CalendarEvent } from "../components/calendar/CalendarEventCard";
import { createDayHeaderComponent } from "../components/calendar/CalendarDayHeader";
import { CalendarDayColumnWrapper } from "../components/calendar/CalendarDayColumnWrapper";
import { CalendarZoomControls } from "../components/calendar/CalendarZoomControls";
import { splitAtMidnight, displayEndForCalendar } from "../utils/calendar-events-builder";
import { calendarDayLayout } from "../utils/calendar-day-layout";
import { getElapsedSeconds } from "../components/LiveDuration";

// ── Zoom configuration ────────────────────────────────────────────────────────

/** Maps zoom level (-1 | 0 | 1) to react-big-calendar step/timeslots. */
const zoomConfig = {
  "-1": { step: 30, timeslots: 2 },
  "0": { step: 15, timeslots: 4 },
  "1": { step: 10, timeslots: 6 },
} as const;

// ── Context menu ──────────────────────────────────────────────────────────────

interface ContextMenuState {
  entry: TimeEntry;
  x: number;
  y: number;
}

interface ContextMenuProps {
  state: ContextMenuState;
  onClose: () => void;
  onEdit: (entry: TimeEntry) => void;
  onContinue: (entry: TimeEntry) => void;
  onDelete: (entry: TimeEntry) => void;
}

function CalendarContextMenu({ state, onClose, onEdit, onContinue, onDelete }: ContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null);

  // Close on Escape key.
  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [onClose]);

  // Close on outside mousedown.
  useEffect(() => {
    const handleMouseDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    document.addEventListener("mousedown", handleMouseDown);
    return () => document.removeEventListener("mousedown", handleMouseDown);
  }, [onClose]);

  const isStopped = state.entry.stop_time !== null;

  const baseItemStyle: React.CSSProperties = {
    display: "block",
    width: "100%",
    textAlign: "left",
    padding: "6px 12px",
    fontSize: "0.8125rem",
    background: "none",
    border: "none",
    cursor: "pointer",
    color: "var(--foreground)",
  };

  return (
    <div
      ref={menuRef}
      style={{
        position: "fixed",
        top: state.y,
        left: state.x,
        zIndex: 50,
        backgroundColor: "var(--background)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius)",
        boxShadow: "0 4px 12px rgba(0,0,0,0.15)",
        minWidth: "140px",
        overflow: "hidden",
      }}
    >
      <button
        type="button"
        style={baseItemStyle}
        onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = "var(--muted)")}
        onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
        onClick={() => {
          onEdit(state.entry);
          onClose();
        }}
      >
        Edit
      </button>
      {isStopped && (
        <button
          type="button"
          style={baseItemStyle}
          onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = "var(--muted)")}
          onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
          onClick={() => {
            onContinue(state.entry);
            onClose();
          }}
        >
          Continue
        </button>
      )}
      <button
        type="button"
        style={{ ...baseItemStyle, color: "var(--destructive)" }}
        onMouseEnter={(e) =>
          (e.currentTarget.style.backgroundColor =
            "color-mix(in srgb, var(--destructive) 10%, transparent)")
        }
        onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
        onClick={() => {
          onDelete(state.entry);
          onClose();
        }}
      >
        Delete
      </button>
    </div>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────────

export function MyTimeCalendarPage() {
  const { data: running } = useCurrentTimerQuery();
  const updateEntry = useUpdateTimeEntryMutation();
  const { requestStart, pendingSwitch, confirmSwitch, setPendingSwitch, requestDelete } = useTimeEntryActions({ currentEntry: running });

  const [view, setView] = useState<View>(Views.DAY);
  const [date, setDate] = useState(new Date());
  const [editingEntry, setEditingEntry] = useState<TimeEntry | null>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [zoom, setZoom] = useState<-1 | 0 | 1>(0);
  const [createSheetOpen, setCreateSheetOpen] = useState(false);
  const [isSwitching, setIsSwitching] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [entryToDelete, setEntryToDelete] = useState<TimeEntry | null>(null);

  // Tracks the current time so the running timer event block updates periodically.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (!running) return;
    const id = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(id);
  }, [!!running]);

  // Compute the visible date window based on the current view and date so the
  // query only fetches what's needed (and never gets capped by a fixed limit).
  const { since, until } = useMemo(() => {
    const d = new Date(date);
    if (view === Views.DAY) {
      const s = new Date(d.getFullYear(), d.getMonth(), d.getDate());
      const u = new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1);
      return { since: s.toISOString(), until: u.toISOString() };
    }
    if (view === Views.WEEK) {
      // Monday-anchored week window (±1 day buffer for cross-midnight entries).
      const dayOfWeek = (d.getDay() + 6) % 7; // 0=Mon
      const mon = new Date(d.getFullYear(), d.getMonth(), d.getDate() - dayOfWeek);
      const sun = new Date(mon.getFullYear(), mon.getMonth(), mon.getDate() + 7);
      return { since: mon.toISOString(), until: sun.toISOString() };
    }
    // Month view fallback.
    const s = new Date(d.getFullYear(), d.getMonth(), 1);
    const u = new Date(d.getFullYear(), d.getMonth() + 1, 1);
    return { since: s.toISOString(), until: u.toISOString() };
  }, [view, date]);

  const { data: entries = [] } = useTimeEntriesQuery({ since, until });

  const calendarRef = useRef<HTMLDivElement>(null);

  // Merge running timer with historical entries, deduplicate by id.
  const allEntries = useMemo<TimeEntry[]>(() => {
    if (!running) return entries;
    const seen = new Set(entries.map((e) => e.id));
    return seen.has(running.id) ? entries : [running, ...entries];
  }, [entries, running]);

  // Convert entries to calendar events, splitting at midnight boundaries.
  const events = useMemo<CalendarEvent[]>(() => {
    const result: CalendarEvent[] = [];
    for (const entry of allEntries) {
      const start = new Date(entry.start_time);
      // Use the reactive `now` for running entries so the block extends as time passes.
      const end = entry.stop_time ? new Date(entry.stop_time) : now;
      const displayEnd = displayEndForCalendar(start, end);
      const segments = splitAtMidnight(start, displayEnd);
      for (const seg of segments) {
        result.push({
          id: entry.id,
          title: entry.description ?? "Time entry",
          start: seg.start,
          end: seg.end,
          resource: entry,
        });
      }
    }
    return result;
  }, [allEntries, now]);

  // Compute daily totals keyed by "YYYY-MM-DD" for the day header component.
  const dailyTotals = useMemo(() => {
    const map = new Map<string, number>();
    for (const entry of allEntries) {
      const dateKey = new Intl.DateTimeFormat("en-CA", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      }).format(new Date(entry.start_time));
      const secs = entry.stop_time ? entry.duration_seconds : getElapsedSeconds(entry);
      map.set(dateKey, (map.get(dateKey) ?? 0) + secs);
    }
    return map;
  }, [allEntries]);

  // Start a new timer continuing the given entry.
  const handleContinueEntry = useCallback(
    async (entry: TimeEntry) => {
      try {
        await requestStart({
          description: entry.description ?? undefined,
          issue_id: entry.issue_id,
          start_time: new Date().toISOString(),
        });
      } catch (error) {
        toast.error("Failed to continue timer");
      }
    },
    [requestStart],
  );

  const handleConfirmSwitch = useCallback(async () => {
    setIsSwitching(true);
    try {
      await confirmSwitch();
    } catch (error) {
      toast.error("Failed to switch timer");
    } finally {
      setIsSwitching(false);
    }
  }, [confirmSwitch]);

  const handleDeleteEntry = useCallback(
    (entry: TimeEntry) => {
      setEntryToDelete(entry);
      setDeleteDialogOpen(true);
    },
    [],
  );

  const handleDeleteConfirm = useCallback(async () => {
    if (!entryToDelete) return;

    requestDelete(entryToDelete, entryToDelete.issue_id);
    setDeleteDialogOpen(false);
    setEntryToDelete(null);
  }, [entryToDelete, requestDelete]);

  // Keep the event component stable unless the continue handler changes.
  const eventComponent = useCallback(
    (props: { event: CalendarEvent }) => (
      <CalendarEventCard
        event={props.event}
        onContextMenu={(entry, x, y) => setContextMenu({ entry, x, y })}
        onContinueEntry={handleContinueEntry}
        onEditEntry={setEditingEntry}
      />
    ),
    [handleContinueEntry],
  );

  // Rebuild the day header only when the visible totals change.
  const dayHeaderComponent = useMemo(
    () => createDayHeaderComponent(dailyTotals, new Date()),
    [dailyTotals],
  );

  // Zoom controls for the timeGutterHeader slot.
  const zoomControls = useCallback(
    () => (
      <CalendarZoomControls
        zoom={zoom}
        onZoomIn={() => setZoom((z) => (z < 1 ? ((z + 1) as -1 | 0 | 1) : z))}
        onZoomOut={() => setZoom((z) => (z > -1 ? ((z - 1) as -1 | 0 | 1) : z))}
      />
    ),
    [zoom],
  );

  /** Starts a new timer from the current day column. */
  const handleStartFromColumn = useCallback(async () => {
    try {
      await requestStart({ start_time: new Date().toISOString() });
    } catch (error) {
      toast.error("Failed to start timer");
    }
  }, [requestStart]);

  // Inject the quick-start button into today's column.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const dayColumnWrapper = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (props: any) => {
      // RBC may pass the date via different prop shapes depending on the view.
      const colDate: Date | undefined =
        props.children?.props?.date ?? props.resource?.date ?? undefined;
      const isNow = colDate ? colDate.toDateString() === new Date().toDateString() : false;
      return (
        <CalendarDayColumnWrapper
          {...props}
          isNow={isNow}
          onStartEntry={() => void handleStartFromColumn()}
        />
      );
    },
    [handleStartFromColumn],
  );

  // Skip drag-and-drop writes for the running entry.
  const handleEventDrop = useCallback(
    ({ event, start, end }: EventInteractionArgs<CalendarEvent>) => {
      if (event.resource.stop_time === null) return;
      updateEntry.mutate({
        id: event.resource.id,
        data: {
          start_time: new Date(start).toISOString(),
          stop_time: new Date(end).toISOString(),
        },
      });
    },
    [updateEntry],
  );

  const handleEventResize = useCallback(
    ({ event, start, end }: EventInteractionArgs<CalendarEvent>) => {
      if (event.resource.stop_time === null) return;
      updateEntry.mutate({
        id: event.resource.id,
        data: {
          start_time: new Date(start).toISOString(),
          stop_time: new Date(end).toISOString(),
        },
      });
    },
    [updateEntry],
  );

  // Auto-scroll to center the current time indicator after mount / navigation.
  useEffect(() => {
    const scrollToNow = () => {
      const content = calendarRef.current?.querySelector<HTMLElement>(".rbc-time-content");
      const indicator = calendarRef.current?.querySelector<HTMLElement>(
        ".rbc-current-time-indicator",
      );
      if (!content || !indicator) return;
      const target = indicator.offsetTop - content.clientHeight / 2;
      content.scrollTop = Math.max(0, target);
    };
    // Wait for RBC to render the time indicator before scrolling.
    const timer = setTimeout(scrollToNow, 200);
    return () => clearTimeout(timer);
  }, [view, date]);

  const { step, timeslots } = zoomConfig[String(zoom) as keyof typeof zoomConfig];

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-6 py-4">
        <div className="flex items-center gap-2">
          <Clock className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">My Time Calendar</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setCreateSheetOpen(true)}
          >
            <Plus className="mr-1.5 size-3.5" />
            Add entry
          </Button>
          <Link to="/my-time" className={buttonVariants({ variant: "outline", size: "sm" })}>
            <List className="mr-1.5 size-3.5" />
            List view
          </Link>
        </div>
      </div>

      {/* Calendar fills the remaining height. */}
      <div className="relative flex-1 overflow-hidden" ref={calendarRef}>
        <BigDnDCalendar<CalendarEvent>
          events={events}
          view={view}
          onView={setView}
          date={date}
          onNavigate={setDate}
          defaultView={Views.DAY}
          views={[Views.DAY, Views.WEEK, Views.MONTH, Views.AGENDA]}
          step={step}
          timeslots={timeslots}
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          dayLayoutAlgorithm={calendarDayLayout as any}
          draggableAccessor={(event) => event.resource.stop_time !== null}
          resizableAccessor={(event) => event.resource.stop_time !== null}
          onEventDrop={handleEventDrop}
          onEventResize={handleEventResize}
          components={{
            event: eventComponent,
            header: dayHeaderComponent,
            timeGutterHeader: zoomControls,
            dayColumnWrapper: dayColumnWrapper,
          }}
          style={{ height: "100%" }}
          // Let CalendarEventCard handle all visuals and strip RBC defaults.
          eventPropGetter={() => ({
            style: {
              backgroundColor: "transparent",
              border: "none",
            },
          })}
        />
      </div>

      {/* Context menu */}
      {contextMenu && (
        <CalendarContextMenu
          state={contextMenu}
          onClose={() => setContextMenu(null)}
          onEdit={setEditingEntry}
          onContinue={handleContinueEntry}
          onDelete={handleDeleteEntry}
        />
      )}

      {/* Edit sheet */}
      <TimeEntryEditSheet entry={editingEntry} onClose={() => setEditingEntry(null)} />

      {/* Create sheet */}
      <TimeEntryCreateSheet open={createSheetOpen} onClose={() => setCreateSheetOpen(false)} />

      {/* Switch confirmation dialog */}
      <ConfirmTimerSwitchDialog
        open={!!pendingSwitch}
        isLoading={isSwitching}
        onCancel={() => setPendingSwitch(null)}
        onConfirm={handleConfirmSwitch}
      />

      {/* Delete confirmation dialog */}
      <TimeEntryDeleteDialog
        open={deleteDialogOpen}
        onCancel={() => {
          setDeleteDialogOpen(false);
          setEntryToDelete(null);
        }}
        onConfirm={handleDeleteConfirm}
      />
    </div>
  );
}
