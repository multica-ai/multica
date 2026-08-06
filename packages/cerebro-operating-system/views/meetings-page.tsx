"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { meetingOptions, settingsOptions, useOpenCycleNote } from "../core/queries";
import { CycleEditor } from "./cycle-editor";
import { CyclePlanner } from "./cycle-planner";
import { CycleYearWheel } from "./cycle-year-wheel";
import { OsPageShell } from "./os-page-shell";

/**
 * Cycles (FIR-3589 item 6): one calendar of every recurring review. Each row is
 * a recurring Note, showing its repeat pattern and when it next meets, with a
 * link straight into the Note. A cycle can be created, re-timed and deleted from
 * here, and a cycle whose Note has not been written yet materialises it on the
 * first open, so "open the plan" always leads somewhere.
 */
export function MeetingsPage({ renderCurrentNote }: { renderCurrentNote?: (noteId: string, back: () => void) => React.ReactNode } = {}) {
  const wsId = useWorkspaceId();
  const meeting = useQuery(meetingOptions(wsId));
  const settings = useQuery(settingsOptions(wsId));
  const openNote = useOpenCycleNote(wsId);
  const [openNoteId, setOpenNoteId] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const cyclesLabel = settings.data?.terminology?.meetings ?? "Cycles";
  const noteTypes = meeting.data?.available_note_types ?? [];
  const availableNoteTypes = noteTypes.filter((noteType) => noteType.enabled && noteType.cadence_unit !== "manual");
  const editingCycle = noteTypes.find((noteType) => noteType.id === editingId);

  // A cycle that has never run has no Note yet. Rather than a dead row, the
  // first open writes the Note from the cycle's template and then opens it.
  function open(noteTypeId: string) {
    const noteType = noteTypes.find((candidate) => candidate.id === noteTypeId);
    if (noteType?.current_note_id) {
      setOpenNoteId(noteType.current_note_id);
      return;
    }
    openNote.mutate(noteTypeId, { onSuccess: (noteId) => { if (noteId) setOpenNoteId(noteId); } });
  }

  const shell = (children: React.ReactNode) => (
    <OsPageShell
      title={cyclesLabel}
      subtitle="Operating cadence — every recurring review on one calendar"
      headerActions={<button type="button" aria-label="Add cycle" onClick={() => { setAdding(true); setEditingId(null); }} className="h-8 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground">+ Add cycle</button>}
    >
      <div className="mx-auto grid max-w-5xl gap-6 p-4 sm:p-6">{children}</div>
    </OsPageShell>
  );

  if (meeting.isError) return shell(<p role="alert" className="text-sm text-destructive">Cycles could not be loaded.</p>);
  if (meeting.isLoading || !meeting.data) return shell(<p className="text-sm text-muted-foreground">Loading Cycles…</p>);

  return shell(
    <>
      {adding && <CycleEditor open onOpenChange={(next) => { if (!next) setAdding(false); }} />}
      {editingCycle && <CycleEditor key={editingCycle.id} cycle={editingCycle} open onOpenChange={(next) => { if (!next) setEditingId(null); }} />}

      {openNoteId && renderCurrentNote?.(openNoteId, () => setOpenNoteId(null))}
      {openNoteId && !renderCurrentNote && <div role="alert" className="rounded-xl border border-destructive/30 p-8 text-center text-sm text-destructive">The selected review Note is unavailable. The planner is still available.</div>}
      {!openNoteId && <CyclePlanner noteTypes={availableNoteTypes} label={cyclesLabel} onOpenNote={open} onEdit={setEditingId} />}
      {!openNoteId && <CycleYearWheel noteTypes={availableNoteTypes} label={cyclesLabel} />}
    </>,
  );
}
