"use client";

// CEREBRO-PATCH(cerebro-notes-inbox-box): TECH-3421 — the Notes box for the
// dynamic inbox. Shows the caller's recent (pinned-first) notes with a quick
// "New note" capture, without leaving the inbox; clicking a note (or creating
// one) opens the full Notes surface deep-linked to that note. Self-contained:
// owns its own data + chrome so the dynamic inbox only has to mount it.
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { NotebookPen, Pin, Plus, X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { cn } from "@multica/ui/lib/utils";
import {
  firstLineTitle,
  recentNotesOptions,
  useCreateNote,
  type Note,
} from "../core";

export interface NotesInboxBoxProps {
  title?: string;
  onRemove: () => void;
  /** Max notes to show; defaults to 6. */
  limit?: number;
  /** Drag handle injected by the dynamic inbox's sortable wrapper. */
  dragHandle?: ReactNode;
}

export function NotesInboxBox({
  title,
  onRemove,
  limit = 6,
  dragHandle,
}: NotesInboxBoxProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: notes = [] } = useQuery(recentNotesOptions(wsId, limit));
  const createNote = useCreateNote();

  const openNote = (id: string) =>
    push(`${paths.notes()}?note=${encodeURIComponent(id)}`);

  const handleNew = async () => {
    const note = await createNote.mutateAsync({ visibility: "private" });
    if (note) openNote(note.id);
    else push(paths.notes());
  };

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card">
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
        {dragHandle}
        <NotebookPen className="size-3.5 text-muted-foreground" />
        <span className="text-xs font-bold uppercase tracking-wide text-muted-foreground">
          {title?.trim() || "Notes"}
        </span>
        <span className="text-xs text-muted-foreground">{notes.length}</span>
        <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
          <button
            type="button"
            className="flex items-center gap-1 rounded px-1.5 py-1 text-xs hover:bg-muted disabled:opacity-50"
            onClick={handleNew}
            disabled={createNote.isPending}
            title="New note"
          >
            <Plus className="size-3.5" />
            New
          </button>
          <button
            type="button"
            className="rounded p-1 hover:bg-muted"
            onClick={onRemove}
            title="Remove section"
          >
            <X className="size-3.5" />
          </button>
        </div>
      </header>

      <div className="divide-y divide-border/60">
        {notes.length === 0 ? (
          <button
            type="button"
            onClick={handleNew}
            className="block w-full px-3 py-3 text-left text-xs text-muted-foreground hover:bg-muted/50"
          >
            No notes yet — capture a thought.
          </button>
        ) : (
          notes.map((n: Note) => (
            <button
              key={n.id}
              type="button"
              onClick={() => openNote(n.id)}
              className={cn(
                "block w-full px-3 py-2 text-left hover:bg-muted/50",
              )}
            >
              <div className="flex items-center gap-1.5 text-[13px] font-medium">
                {n.pinned && <Pin className="size-3 shrink-0 text-amber-500" />}
                <span className="truncate">{firstLineTitle(n)}</span>
              </div>
              <div className="mt-0.5 line-clamp-1 text-xs text-muted-foreground">
                {notePreview(n)}
              </div>
            </button>
          ))
        )}
      </div>
    </section>
  );
}

function notePreview(note: Note): string {
  const lines = note.body.split("\n").filter((l) => l.trim().length > 0);
  const skipTitle = !note.title.trim() && lines.length > 0 ? 1 : 0;
  return lines.slice(skipTitle).join(" ") || "Empty note";
}
