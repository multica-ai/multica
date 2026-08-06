"use client";

import { CalendarClock, ChevronRight, Pencil } from "lucide-react";
import { planRecurringNotes } from "../core/cycle-planner";
import type { MeetingNoteType } from "../core/types";

interface CyclePlannerProps {
  /** Every recurring Note type in the workspace. */
  noteTypes: MeetingNoteType[];
  /** Terminology label for cycles, used for the section's accessible name. */
  label: string;
  /** Open the recurring Note behind a planner row, by cycle id. The page
   *  resolves it — a cycle that has never run has its Note written on first
   *  open, so no row is a dead end. */
  onOpenNote?: (noteTypeId: string) => void;
  /** Open a cycle's timing for editing (FIR-3589 item 6). */
  onEdit?: (noteTypeId: string) => void;
  /** Test seam: pins "today" so the projected schedule is deterministic. */
  today?: string;
}

/**
 * The recurring planner: a single calendar of every recurring Note, each with
 * its next meeting date and a link into the Note. Replaces the old single-cadence
 * Cycle timeline (FIR-3589 item 6).
 */
export function CyclePlanner({ noteTypes, label, onOpenNote, onEdit, today }: CyclePlannerProps) {
  const planned = planRecurringNotes(noteTypes, today);

  return (
    <section aria-label={`${label} planner`} className="grid gap-3 rounded-xl border bg-card p-5">
      <div>
        <h2 className="font-semibold">Planner</h2>
        <p className="text-sm text-muted-foreground">
          Every recurring Note and when it next meets. Open a Note to run its cycle in context.
        </p>
      </div>

      {planned.length === 0 ? (
        <p className="rounded-lg border border-dashed bg-background p-6 text-center text-sm text-muted-foreground">
          No cycles yet. Add one — a name and how often it meets — and it appears here.
        </p>
      ) : (
        <ol className="grid gap-2">
          {planned.map(({ noteType, nextLabel, relative, summary, upcoming }) => {
            const openable = Boolean(onOpenNote);
            return (
              <li key={noteType.id} className="relative">
                <button
                  type="button"
                  aria-label={`Open ${noteType.name}`}
                  disabled={!openable}
                  onClick={() => onOpenNote?.(noteType.id)}
                  className="grid w-full grid-cols-[auto_1fr_auto] items-center gap-3 rounded-lg border bg-background p-3 pr-16 text-left enabled:hover:border-primary/50 disabled:cursor-default"
                >
                  <span className="grid size-9 place-items-center rounded-md bg-primary/10 text-primary" aria-hidden>
                    <CalendarClock className="size-4" />
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{noteType.name}</span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {summary}
                      {noteType.participants && noteType.participants.length > 0 && ` · ${noteType.participants.length} ${noteType.participants.length === 1 ? "participant" : "participants"}`}
                    </span>
                    {upcoming.length > 0 && (
                      <span className="mt-0.5 block truncate text-xs text-muted-foreground/80">
                        Then {upcoming.join(" · ")}
                      </span>
                    )}
                  </span>
                  <span className="flex items-center gap-2 justify-self-end text-right">
                    <span className="grid gap-0.5">
                      <span className="text-xs font-semibold uppercase tracking-wide text-primary">
                        {relative ?? "Not scheduled"}
                      </span>
                      {nextLabel && <span className="text-sm font-medium">{nextLabel}</span>}
                    </span>
                    {openable && <ChevronRight aria-hidden className="size-4 text-muted-foreground" />}
                  </span>
                </button>
                {onEdit && (
                  <button
                    type="button"
                    aria-label={`Edit ${noteType.name}`}
                    onClick={() => onEdit(noteType.id)}
                    className="absolute right-2 top-1/2 grid size-9 -translate-y-1/2 place-items-center rounded-md border bg-background text-muted-foreground hover:text-foreground"
                  >
                    <Pencil aria-hidden className="size-4" />
                  </button>
                )}
              </li>
            );
          })}
        </ol>
      )}
    </section>
  );
}
