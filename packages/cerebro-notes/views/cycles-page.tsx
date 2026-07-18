"use client";

// CEREBRO-PATCH(operating-system-cycle-note-composition): FIR-2816 composes the
// fork-owned Cycle shell with the canonical Note editor in the Notes package,
// which already owns the editor and can depend on the leaf Operating System UI.
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { MeetingsPage } from "@multica/cerebro-operating-system/views";

import { noteDetailOptions } from "../core";
import { NoteEditor } from "./notes-page";

function CurrentCycleNote({ noteId, openSetup }: { noteId: string; openSetup: () => void }) {
  const wsId = useWorkspaceId();
  const note = useQuery(noteDetailOptions(wsId, noteId));
  if (note.isLoading) return <div className="rounded-xl border p-8 text-center text-sm text-muted-foreground">Loading current review Note…</div>;
  if (note.isError || !note.data) return <div role="alert" className="rounded-xl border border-destructive/30 p-8 text-center text-sm text-destructive">The current review Note could not be loaded. Cycle setup is still available.</div>;
  return <div className="min-h-[32rem] overflow-hidden rounded-xl border bg-card"><NoteEditor key={note.data.id} note={note.data} wsId={wsId} onBack={openSetup} /></div>;
}

export function CyclesPage() {
  return <MeetingsPage renderCurrentNote={(noteId, openSetup) => <CurrentCycleNote key={noteId} noteId={noteId} openSetup={openSetup} />} />;
}
