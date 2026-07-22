"use client";

import { buildCycleTimeline, formatCycleDate } from "../core/cycle-timeline";
import type { MeetingCadenceUnit } from "../core/types";

interface CycleTimelineProps {
  cadenceUnit: MeetingCadenceUnit;
  cadenceCount: number;
  /** Terminology label for cycles, used for the section's accessible name. */
  label: string;
  /** Test seam: pins "today" so the projected schedule is deterministic. */
  today?: string;
}

function relativeLabel(offset: number): string {
  if (offset === 0) return "Current";
  if (offset === -1) return "Previous";
  if (offset === 1) return "Next";
  if (offset < 0) return `${Math.abs(offset)} cycles ago`;
  return `In ${offset} cycles`;
}

export function CycleTimeline({ cadenceUnit, cadenceCount, label, today }: CycleTimelineProps) {
  const occurrences = buildCycleTimeline(cadenceUnit, cadenceCount, today, { past: 1, upcoming: 5 });
  if (occurrences.length === 0) return null;

  return (
    <section aria-label={`${label} timeline`} className="grid gap-3 rounded-xl border bg-card p-5">
      <div>
        <h2 className="font-semibold">Timeline</h2>
        <p className="text-sm text-muted-foreground">Recurring cycles projected from the selected Note&apos;s timing. Plan ahead — the current cycle is highlighted.</p>
      </div>
      <ol className="flex gap-3 overflow-x-auto pb-1">
        {occurrences.map((occ) => (
          <li key={occ.offset} className={`flex min-w-[8.5rem] shrink-0 flex-col gap-1 rounded-lg border p-3 ${occ.relative === "current" ? "border-primary bg-primary/5" : occ.relative === "past" ? "opacity-60" : "bg-background"}`}>
            <span className={`text-xs font-semibold uppercase tracking-wide ${occ.relative === "current" ? "text-primary" : "text-muted-foreground"}`}>{relativeLabel(occ.offset)}</span>
            <span className="text-sm font-medium">{formatCycleDate(occ.date)}</span>
          </li>
        ))}
      </ol>
    </section>
  );
}
