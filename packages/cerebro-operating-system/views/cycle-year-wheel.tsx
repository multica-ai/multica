"use client";

import { buildYearWheel } from "../core/cycle-year-wheel";
import type { MeetingNoteType } from "../core/types";

interface CycleYearWheelProps {
  /** Every recurring Note type in the workspace. */
  noteTypes: MeetingNoteType[];
  /** Terminology label for cycles, used for the section's accessible name. */
  label: string;
  /** Test seam: pins "today" so the rolling year is deterministic. */
  today?: string;
}

/**
 * The year-wheel (årshjul): a rolling 12-month overview of the operating
 * cadence, so you can see the whole year of cycles at a glance (FIR-3589).
 */
export function CycleYearWheel({ noteTypes, label, today }: CycleYearWheelProps) {
  const months = buildYearWheel(noteTypes, today);
  const hasAny = months.some((month) => month.entries.length > 0);
  if (!hasAny) return null;

  return (
    <section aria-label={`${label} year wheel`} className="grid gap-3 rounded-xl border bg-card p-5">
      <div>
        <h2 className="font-semibold">Year wheel</h2>
        <p className="text-sm text-muted-foreground">The next 12 months of cycles at a glance.</p>
      </div>
      <ol className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
        {months.map((month) => (
          <li key={month.key} className={`grid gap-1.5 rounded-lg border p-3 ${month.entries.length === 0 ? "opacity-60" : "bg-background"}`}>
            <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{month.label}</span>
            {month.entries.length === 0 ? (
              <span className="text-xs text-muted-foreground/70">—</span>
            ) : (
              <ul className="grid gap-1">
                {month.entries.map((entry) => (
                  <li key={entry.noteType.id} className="flex items-baseline justify-between gap-2 text-xs">
                    <span className="min-w-0 truncate font-medium">{entry.noteType.name}</span>
                    <span className="shrink-0 tabular-nums text-muted-foreground">{entry.days.join(", ")}</span>
                  </li>
                ))}
              </ul>
            )}
          </li>
        ))}
      </ol>
    </section>
  );
}
