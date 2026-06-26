import { type ReactNode } from "react";
import { useTimeAgo } from "../i18n/use-time-ago";
import { HoverTooltip } from "./instant-tooltip";
import { useFormatCalendarDate } from "./use-format-calendar-date";
import {
  useFormatDateTime,
  useFormatInstantTooltip,
} from "./use-format-date-time";

type InstantVariant = "datetime" | "date" | "time" | "relative";

export type DateTimeVariant = InstantVariant | "calendarDate";

interface DateTimeProps {
  value: string | null | undefined;
  /** What the visible text shows. Defaults to "datetime". */
  variant?: DateTimeVariant;
  className?: string;
  tooltipSide?: "top" | "right" | "bottom" | "left";
  /** Render nothing when value is empty/unparseable (default true). */
  hideWhenEmpty?: boolean;
}

// Full-date tooltip for floating calendar days: no time, no timezone.
const CALENDAR_TOOLTIP_OPTS: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "long",
  day: "numeric",
};

// Each variant is its own leaf so it calls ONLY the formatting hook it needs —
// a relative row never builds the calendar formatter, a calendar row never
// builds the instant/relative ones. In long lists (inbox, comments, board)
// that drops one or two unused `useT` reads + memos per row. The thin DateTime
// dispatcher below picks the leaf; all share the lazy-tooltip wrapper so the
// full-precision tooltip is formatted only on hover.

type LeafProps = Omit<DateTimeProps, "variant">;

interface LeafChrome {
  className?: string;
  tooltipSide?: "top" | "right" | "bottom" | "left";
  enabled: boolean;
  hideWhenEmpty: boolean;
}

// Shared tail every leaf ends with: drop an empty value, else wrap the visible
// text in the lazy hover tooltip. One place so the empty-handling, `enabled`
// gate, and tooltip wiring can't drift between variants.
function renderLeaf(
  visible: string,
  content: () => string,
  { className, tooltipSide, enabled, hideWhenEmpty }: LeafChrome,
): ReactNode {
  if (!visible && hideWhenEmpty) return null;
  return (
    <HoverTooltip
      className={className}
      tooltipSide={tooltipSide}
      enabled={enabled}
      content={content}
    >
      {visible}
    </HoverTooltip>
  );
}

function CalendarDateTime({
  value,
  className,
  tooltipSide,
  hideWhenEmpty = true,
}: LeafProps) {
  const formatCalendarDate = useFormatCalendarDate();
  const visible = formatCalendarDate(value ?? undefined);
  return renderLeaf(
    visible,
    () => formatCalendarDate(value ?? undefined, CALENDAR_TOOLTIP_OPTS),
    { className, tooltipSide, enabled: value != null, hideWhenEmpty },
  );
}

function RelativeDateTime({
  value,
  className,
  tooltipSide,
  hideWhenEmpty = true,
}: LeafProps) {
  const timeAgo = useTimeAgo();
  const formatTooltip = useFormatInstantTooltip();
  const visible = value ? timeAgo(value) : "";
  return renderLeaf(visible, () => formatTooltip(value), {
    className,
    tooltipSide,
    enabled: value != null,
    hideWhenEmpty,
  });
}

function InstantDateTime({
  value,
  variant,
  className,
  tooltipSide,
  hideWhenEmpty = true,
}: LeafProps & { variant: "datetime" | "date" | "time" }) {
  const { formatDateTime, formatDate, formatTime, formatTooltip } =
    useFormatDateTime();
  let visible: string;
  switch (variant) {
    case "date":
      visible = formatDate(value);
      break;
    case "time":
      visible = formatTime(value);
      break;
    default:
      visible = formatDateTime(value);
  }
  return renderLeaf(visible, () => formatTooltip(value), {
    className,
    tooltipSide,
    enabled: value != null,
    hideWhenEmpty,
  });
}

/**
 * Unified time display for web/desktop: visible text + a tooltip carrying the
 * full time and timezone. Instants render in the viewer's Viewing Timezone +
 * Language locale; the "calendarDate" variant renders a floating day (UTC
 * anchored, no timezone) with a date-only tooltip.
 */
export function DateTime({
  value,
  variant = "datetime",
  className,
  tooltipSide = "top",
  hideWhenEmpty = true,
}: DateTimeProps) {
  const common = { value, className, tooltipSide, hideWhenEmpty };
  switch (variant) {
    case "calendarDate":
      return <CalendarDateTime {...common} />;
    case "relative":
      return <RelativeDateTime {...common} />;
    default:
      return <InstantDateTime {...common} variant={variant} />;
  }
}
