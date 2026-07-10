"use client";

// CEREBRO-PATCH(cerebro-note-inbox-detail): TECH-3690 (Jesper) — opening a note
// from the inbox Notes box renders it in the SAME detail pane that messages use,
// instead of navigating away to the full Notes page. This wrapper loads the
// note by id and mounts the shared NoteEditor (full read+edit) inside the pane,
// with an "Open full" button that deep-links to the full Notes surface when the
// user wants more room. The dynamic inbox passes a note id, and (FIR-2826) the
// mentioned comment id when the row came from a note-comment mention.
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { noteDetailOptions } from "../core";
import { NoteEditor } from "./notes-page";

export interface NoteInboxDetailProps {
  /** The note to show in the inbox detail pane. */
  noteId: string;
  /**
   * FIR-2826 — when the note was opened from a note-comment mention, the
   * mentioned comment id. Forwarded to NoteEditor, which opens the comments
   * panel and scrolls to that comment.
   */
  initialCommentId?: string | null;
  /** Close the pane (clear the inbox selection / return to the list on mobile). */
  onClose: () => void;
}

export function NoteInboxDetail({
  noteId,
  initialCommentId,
  onClose,
}: NoteInboxDetailProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: note, isLoading } = useQuery(noteDetailOptions(wsId, noteId));

  if (isLoading) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 text-sm text-muted-foreground">
        Loading note…
      </div>
    );
  }

  if (!note) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 text-sm text-muted-foreground">
        Note not found.
      </div>
    );
  }

  return (
    <NoteEditor
      key={note.id}
      note={note}
      wsId={wsId}
      initialCommentId={initialCommentId}
      onBack={onClose}
      onOpenFull={() =>
        // FIR-2826 — keep the comment deep-link when jumping to the full page.
        push(
          `${paths.noteDetail(note.id)}${
            initialCommentId
              ? `?comment=${encodeURIComponent(initialCommentId)}`
              : ""
          }`,
        )
      }
    />
  );
}
