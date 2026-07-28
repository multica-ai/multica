"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { meetingOptions, settingsOptions } from "../core/queries";
import { CyclePlanner } from "./cycle-planner";
import { CycleYearWheel } from "./cycle-year-wheel";

/**
 * Cycles (FIR-3589 item 6): one calendar of every recurring review. Each row is
 * a recurring Note, showing its repeat pattern and when it next meets, with a
 * link straight into the Note. The repeat itself is set on the Note (weekly,
 * "Nth weekday of the month", etc.). The old per-meeting agenda / binding setup
 * has been removed — Cycles is purely the planner now.
 */
export function MeetingsPage({ renderCurrentNote }: { renderCurrentNote?: (noteId: string, back: () => void) => React.ReactNode } = {}) {
  const wsId = useWorkspaceId();
  const meeting = useQuery(meetingOptions(wsId));
  const settings = useQuery(settingsOptions(wsId));
  const [openNoteId, setOpenNoteId] = useState<string | null>(null);

  if (meeting.isError) return <div role="alert" className="mx-auto max-w-4xl p-6 text-sm text-destructive">Cycles could not be loaded.</div>;
  if (meeting.isLoading || !meeting.data) return <div className="mx-auto max-w-4xl p-6 text-sm text-muted-foreground">Loading Cycles…</div>;

  const availableNoteTypes = meeting.data.available_note_types.filter((noteType) => noteType.enabled && noteType.cadence_unit !== "manual");
  const cyclesLabel = settings.data?.terminology?.meetings ?? "Cycles";

  return (
    <div className="mx-auto grid h-full max-w-5xl gap-6 overflow-y-auto p-4 sm:p-6">
      <div>
        <p className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">Operating cadence</p>
        <h1 className="mt-1 text-2xl font-semibold">{cyclesLabel}</h1>
        <p className="mt-1 text-sm text-muted-foreground">Every recurring review on one calendar — when each meets next, with a link straight into its Note.</p>
      </div>
      {openNoteId && renderCurrentNote?.(openNoteId, () => setOpenNoteId(null))}
      {openNoteId && !renderCurrentNote && <div role="alert" className="rounded-xl border border-destructive/30 p-8 text-center text-sm text-destructive">The selected review Note is unavailable. The planner is still available.</div>}
      {!openNoteId && <CyclePlanner noteTypes={availableNoteTypes} label={cyclesLabel} onOpenNote={setOpenNoteId} />}
      {!openNoteId && <CycleYearWheel noteTypes={availableNoteTypes} label={cyclesLabel} />}
    </div>
  );
}
