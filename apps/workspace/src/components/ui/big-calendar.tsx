/**
 * BigCalendar — themed wrapper around react-big-calendar.
 *
 * Applies shadcn/Tailwind design tokens via CSS variables so the calendar
 * integrates with the app's light/dark theme automatically.
 *
 * Usage:
 *   import { BigCalendar } from "@/components/ui/big-calendar";
 *   <BigCalendar events={events} defaultView="month" ... />
 */
import "react-big-calendar/lib/css/react-big-calendar.css";

import { dateFnsLocalizer } from "react-big-calendar";
import { format, parse, startOfWeek, getDay } from "date-fns";
import { enUS } from "date-fns/locale";
import {
  Calendar as RBCalendar,
  type CalendarProps,
} from "react-big-calendar";

// ── date-fns localizer (reused by all calender instances) ─────────────────────
const localizer = dateFnsLocalizer({
  format,
  parse,
  startOfWeek: () => startOfWeek(new Date(), { locale: enUS }),
  getDay,
  locales: { "en-US": enUS },
});

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
    <div className={`rbc-theme ${className ?? ""}`}>
      <RBCalendar<TEvent> localizer={localizer} {...props} />
    </div>
  );
}
