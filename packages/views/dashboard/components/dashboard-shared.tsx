"use client";

import {
  NumberFlow,
  NumberFlowGroup,
} from "@multica/ui/components/ui/number-flow";
import { formatDuration } from "../utils";

// Period selector — mirrors the runtime detail page so users see the same
// option set across both dashboards. `dims` declares which chart dimensions
// each range may be drawn at: 1d / 7d at the weekly grain collapse to a single
// bar, 180d at the daily grain is 180 unreadable bars.
//
// 1d semantic: "today" (the natural calendar day from 00:00 in the viewer's
// timezone), not "the last 24 hours". The `dailyCutoffIso` filter on the page
// enforces this even at the midnight edge.
export const TIME_RANGES = [
  { label: "1d", days: 1, dims: ["daily"] as const },
  { label: "7d", days: 7, dims: ["daily"] as const },
  { label: "30d", days: 30, dims: ["daily", "weekly"] as const },
  { label: "90d", days: 90, dims: ["daily", "weekly"] as const },
  { label: "180d", days: 180, dims: ["weekly"] as const },
] as const;

export type TimeRange = (typeof TIME_RANGES)[number]["days"];
export type Dim = "daily" | "weekly";

/**
 * Which chart dimensions the current range may be drawn at.
 *
 * The constraint between range and dimension used to be enforced in both
 * directions by two sibling controls in the page header: picking Weekly while
 * on 1d silently reset the range to 90d, which moved every KPI on the page.
 * The range is now the page-scoped filter and the dimension is card-scoped, so
 * the dependency runs one way only — a card offers whichever dimensions its
 * range allows, and nothing resets. A card-scoped control must never reach up
 * and change a page-scoped one (MUL-5759).
 */
export function dimsForDays(days: TimeRange): readonly Dim[] {
  return (
    TIME_RANGES.find((r) => r.days === days)?.dims ?? (["daily"] as const)
  );
}

/** Sentinel for "no project filter" — kept distinct from the empty string so
 *  it survives a refactor that ever lets a project be slug-keyed. */
export const ALL_PROJECTS = "__all__";

/**
 * Card-local segmented control — the pill toggle used *inside* a card to pick
 * what that card shows. The page header deliberately uses outline Buttons
 * instead, so the shape of a control tells you its scope: pills change one
 * card, buttons change the page.
 *
 * shadcn's Tabs is wired for full tab pages with ARIA semantics the compact
 * toolbar pill doesn't need. Which option is active was expressed only as a
 * colour swap, which no screen reader can see, so `aria-pressed` carries it
 * too. `label` is required rather than optional because a naked group of
 * toggle buttons is announced without saying WHAT it toggles — "Rate, pressed"
 * is useless until you know the group is the offender ranking. Toggle buttons
 * rather than a radiogroup: a radiogroup owes the user arrow-key roving focus,
 * and these are tab stops wherever they appear in the page.
 */
export function Segmented<T extends string | number>({
  value,
  onChange,
  options,
  label,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { label: string; value: T }[];
  label: string;
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5"
    >
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
          className={`rounded-sm px-2.5 py-1 text-caption font-medium transition-colors ${
            o.value === value
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function DurationNumberFlow({
  seconds,
  lessThanMinuteLabel,
  locales,
}: {
  seconds: number;
  lessThanMinuteLabel: string;
  locales?: Intl.LocalesArgument;
}) {
  const label = formatDuration(seconds, lessThanMinuteLabel);
  const parts = Array.from(label.matchAll(/(\d+)([a-z]+)/gi), (match) => ({
    value: Number(match[1]),
    unit: match[2] ?? "",
  }));

  if (parts.length === 0) return label;

  return (
    <>
      <span className="sr-only">{label}</span>
      <NumberFlowGroup>
        <span aria-hidden className="inline-flex items-baseline gap-1">
          {parts.map((part) => (
            <NumberFlow
              key={part.unit}
              value={part.value}
              locales={locales}
              suffix={part.unit}
              format={{ maximumFractionDigits: 0, useGrouping: false }}
            />
          ))}
        </span>
      </NumberFlowGroup>
    </>
  );
}
