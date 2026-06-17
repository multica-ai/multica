"use client";

// CEREBRO-PATCH(cerebro-note-inbox-detail): TECH-3690 (Jesper) — opening a note
// from the inbox Notes box renders it in the SAME detail pane that messages use,
// instead of navigating away to the full Notes page. This wrapper loads the
// note by id and mounts the shared NoteEditor (full read+edit) inside the pane,
// with an "Åbn fuldt" button that deep-links to the full Notes surface when the
// user wants more room. Self-contained: the dynamic inbox only passes a note id.
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { noteDetailOptions } from "../core";
import { NoteEditor } from "./notes-page";

export interface NoteInboxDetailProps {
  /** The note to show in the inbox detail pane. */
  noteId: string;
  /** Close the pane (clear the inbox selection / return to the list on mobile). */
  onClose: () => void;
}

export function NoteInboxDetail({ noteId, onClose }: NoteInboxDetailProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: note, isLoading } = useQuery(noteDetailOptions(wsId, noteId));

  if (isLoading) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 text-sm text-muted-foreground">
        Indlæser note…
      </div>
    );
  }

  if (!note) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 text-sm text-muted-foreground">
        Noten blev ikke fundet.
      </div>
    );
  }

  return (
    <NoteEditor
      key={note.id}
      note={note}
      wsId={wsId}
      onBack={onClose}
      onOpenFull={() =>
        push(`${paths.notes()}?note=${encodeURIComponent(note.id)}`)
      }
    />
  );
}
