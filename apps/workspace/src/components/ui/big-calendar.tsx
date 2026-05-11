/**
 * BigCalendar — themed wrapper around react-big-calendar.
 *
 * Applies shadcn/Tailwind design tokens via CSS variables so the calendar
 * integrates with the app's light/dark theme automatically.
 *
 * Usage:
 *   import { BigCalendar } from "@/components/ui/big-calendar";
 *   <BigCalendar events={events} defaultView="month" ... />
 *
 * DnD variant:
 *   import { BigDnDCalendar } from "@/components/ui/big-calendar";
 *   <BigDnDCalendar events={events} onEventDrop={...} onEventResize={...} ... />
 */
import "react-big-calendar/lib/css/react-big-calendar.css";
import "react-big-calendar/lib/addons/dragAndDrop/styles.css";

import React from "react";
import { dateFnsLocalizer } from "react-big-calendar";
import { format, parse, startOfWeek, getDay } from "date-fns";
import { enUS } from "date-fns/locale";
import {
  Calendar as RBCalendar,
  type CalendarProps,
} from "react-big-calendar";
import dndModule from "react-big-calendar/lib/addons/dragAndDrop";
import type {
  withDragAndDropProps,
  EventInteractionArgs,
} from "react-big-calendar/lib/addons/dragAndDrop";

// Resolve CJS default — Vite may double-wrap: { default: fn } or fn directly.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function resolveDefault(mod: any): (component: any) => any {
  if (typeof mod === "function") return mod;
  if (typeof mod?.default === "function") return mod.default;
  throw new Error("react-big-calendar withDragAndDrop could not be resolved");
}
const withDragAndDrop = resolveDefault(dndModule);

// ── date-fns localizer (reused by all calender instances) ─────────────────────
const localizer = dateFnsLocalizer({
  format,
  parse,
  startOfWeek: () => startOfWeek(new Date(), { locale: enUS }),
  getDay,
  locales: { "en-US": enUS },
});

// DnD-wrapped calendar created once at module level to avoid re-creation on render.
const DnDRBCalendar = withDragAndDrop(RBCalendar);

// ── Themed component ──────────────────────────────────────────────────────────

export type BigCalendarProps<TEvent extends object = object> = Omit<
  CalendarProps<TEvent>,
  "localizer"
> & {
  /** Optional extra className on the outer container. */
  className?: string;
};

/**
 * Drop-in calendar component with shadcn theme applied.
 * Pass any react-big-calendar props; localizer is pre-wired.
 */
export function BigCalendar<TEvent extends object = object>({
  className,
  ...props
}: BigCalendarProps<TEvent>) {
  return (
    // h-full ensures height: "100%" on the inner Calendar resolves correctly.
    <div className={`rbc-theme h-full ${className ?? ""}`}>
      <RBCalendar<TEvent> localizer={localizer} {...props} />
    </div>
  );
}

// ── DnD-enabled variant ───────────────────────────────────────────────────────

export type BigDnDCalendarProps<TEvent extends object = object> = Omit<
  CalendarProps<TEvent>,
  "localizer"
> &
  withDragAndDropProps<TEvent> & {
    /** Optional extra className on the outer container. */
    className?: string;
  };

/**
 * Drag-and-drop enabled calendar component with shadcn theme applied.
 * Supports `onEventDrop`, `onEventResize`, `draggableAccessor`, `resizableAccessor`.
 */
export function BigDnDCalendar<TEvent extends object = object>({
  className,
  ...props
}: BigDnDCalendarProps<TEvent>) {
  // DnDRBCalendar doesn't support generic type arguments in JSX — cast to satisfy TypeScript.
  const Calendar = DnDRBCalendar as React.ComponentType<
    BigDnDCalendarProps<TEvent> & { localizer: typeof localizer }
  >;
  return (
    // h-full ensures height: "100%" on the inner Calendar resolves correctly.
    <div className={`rbc-theme h-full ${className ?? ""}`}>
      <Calendar localizer={localizer} {...props} />
    </div>
  );
}

export type { EventInteractionArgs };
