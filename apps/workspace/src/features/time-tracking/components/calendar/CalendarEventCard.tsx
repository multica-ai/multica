/**
 * CalendarEventCard — memo-wrapped event card for react-big-calendar's `components.event` slot.
 *
 * Renders time entry description, duration (live for running entries), and a hover
 * "Continue" play button for stopped entries.
 */
import React, { memo, useState } from "react";
import type { TimeEntry } from "@/shared/types";
import { LiveDuration } from "../LiveDuration";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface CalendarEvent {
  id: string;
  title: string;
  start: Date;
  end: Date;
  resource: TimeEntry;
}

interface CalendarEventCardProps {
  event: CalendarEvent;
  onContextMenu?: (entry: TimeEntry, x: number, y: number) => void;
  onContinueEntry?: (entry: TimeEntry) => void;
  onEditEntry?: (entry: TimeEntry) => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

function CalendarEventCardImpl({
  event,
  onContextMenu,
  onContinueEntry,
  onEditEntry,
}: CalendarEventCardProps) {
  const [hovered, setHovered] = useState(false);
  const entry = event.resource;
  const isRunning = entry.stop_time === null;

  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    onContextMenu?.(entry, e.clientX, e.clientY);
  };

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onEditEntry?.(entry);
  };

  const handleContinueClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onContinueEntry?.(entry);
  };

  return (
    <div
      className={isRunning ? "calendar-event-running" : ""}
      style={{
        overflow: "hidden",
        height: "100%",
        padding: "2px 4px",
        fontSize: "0.75rem",
        position: "relative",
        cursor: "pointer",
        backgroundColor: isRunning ? undefined : "color-mix(in srgb, var(--primary) 22%, var(--background))",
        borderLeft: "3px solid var(--primary)",
        borderRadius: "3px",
        boxShadow: "0 1px 3px rgba(0,0,0,0.12)",
        color: "var(--foreground)",
      }}
      onClick={handleClick}
      onContextMenu={handleContextMenu}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {/* Description */}
      <div style={{ overflow: "hidden", whiteSpace: "nowrap", textOverflow: "ellipsis", fontWeight: 500 }}>
        {entry.description ? (
          entry.description
        ) : (
          <em style={{ opacity: 0.6 }}>No description</em>
        )}
      </div>

      {/* Duration */}
      <LiveDuration entry={entry} className="text-muted-foreground" />

      {/* Continue button — only for stopped entries, shown on hover */}
      {!isRunning && hovered && (
        <button
          type="button"
          title="Continue"
          onClick={handleContinueClick}
          style={{
            position: "absolute",
            top: "2px",
            right: "4px",
            background: "var(--primary)",
            color: "var(--primary-foreground)",
            border: "none",
            borderRadius: "50%",
            width: "16px",
            height: "16px",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            cursor: "pointer",
            padding: 0,
            fontSize: "8px",
          }}
        >
          ▶
        </button>
      )}
    </div>
  );
}

export const CalendarEventCard = memo(CalendarEventCardImpl);
