"use client";

// CEREBRO-PATCH(cerebro-notes-inbox-box): TECH-3421 — the Notes box for the
// dynamic inbox. Shows the caller's recent (pinned-first) notes with a quick
// "New note" capture, without leaving the inbox; clicking a note (or creating
// one) opens the full Notes surface deep-linked to that note. Self-contained:
// owns its own data + chrome so the dynamic inbox only has to mount it.
//
// TECH-3690 (Jesper) — the box was the only inbox block without a settings
// menu. It now has one (Settings2 dropdown), mirroring the Chat/Team block:
// how many notes to show, pinned-only, sort (pinned-first / latest changed /
// newest), and which visibilities to include (private / shared / workspace).
// All filtering is client-side over a fetch window so no API/server change is
// needed — the box is a top-N widget, not a full browser.
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { NotebookPen, Pin, Plus, Settings2, X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuGroup,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  applyBoxSettings,
  firstLineTitle,
  notesListOptions,
  useCreateNote,
  type Note,
  type NotesBoxSort,
  type NoteVisibility,
} from "../core";

export type { NotesBoxSort } from "../core";

const DEFAULT_LIMIT = 6;

const LIMIT_OPTIONS: number[] = [3, 6, 10, 15, 20];

const SORT_OPTIONS: Array<{ label: string; value: NotesBoxSort }> = [
  { label: "Pinned first", value: "pinned" },
  { label: "Latest changed", value: "updated" },
  { label: "Newest", value: "created" },
];

const VISIBILITY_OPTIONS: Array<{ label: string; value: NoteVisibility }> = [
  { label: "Private", value: "private" },
  { label: "Shared", value: "shared" },
  { label: "Workspace", value: "workspace" },
];

export interface NotesInboxBoxProps {
  title?: string;
  onRemove: () => void;
  /** Max notes to show; defaults to 6. */
  limit?: number;
  /** TECH-3690 — only show pinned notes. Default false. */
  pinnedOnly?: boolean;
  /** TECH-3690 — sort order. Default "pinned". */
  sort?: NotesBoxSort;
  /** TECH-3690 — which visibilities to include. undefined / empty = all. */
  visibility?: NoteVisibility[];
  /** TECH-3690 — settings callbacks; when all omitted, the gear is hidden. */
  onSetLimit?: (n: number) => void;
  onSetPinnedOnly?: (v: boolean) => void;
  onSetSort?: (s: NotesBoxSort) => void;
  onSetVisibility?: (v: NoteVisibility[]) => void;
  /** Drag handle injected by the dynamic inbox's sortable wrapper. */
  dragHandle?: ReactNode;
}

export function NotesInboxBox({
  title,
  onRemove,
  limit = DEFAULT_LIMIT,
  pinnedOnly = false,
  sort = "pinned",
  visibility,
  onSetLimit,
  onSetPinnedOnly,
  onSetSort,
  onSetVisibility,
  dragHandle,
}: NotesInboxBoxProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  // Fetch a generous window, then filter/sort/slice client-side. The window is
  // bounded so the box stays a lightweight top-N widget.
  const fetchWindow = Math.min(100, Math.max(50, limit));
  const { data: allNotes = [] } = useQuery(
    notesListOptions(wsId, { limit: fetchWindow }),
  );
  const createNote = useCreateNote();

  const notes = applyBoxSettings(allNotes, {
    limit,
    pinnedOnly,
    sort,
    visibility,
  });

  const hasSettings = Boolean(
    onSetLimit || onSetPinnedOnly || onSetSort || onSetVisibility,
  );

  const openNote = (id: string) =>
    push(`${paths.notes()}?note=${encodeURIComponent(id)}`);

  const handleNew = async () => {
    const note = await createNote.mutateAsync({ visibility: "private" });
    if (note) openNote(note.id);
    else push(paths.notes());
  };

  const toggleVisibility = (v: NoteVisibility) => {
    if (!onSetVisibility) return;
    const current = visibility ?? [];
    const next = current.includes(v)
      ? current.filter((x) => x !== v)
      : [...current, v];
    onSetVisibility(next);
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
          {hasSettings && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type="button"
                    className="rounded p-1 hover:bg-muted"
                    title="Settings"
                  />
                }
              >
                <Settings2 className="size-3.5" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                {onSetSort && (
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>Sort by</DropdownMenuLabel>
                    {SORT_OPTIONS.map((opt) => (
                      <DropdownMenuItem
                        key={opt.value}
                        onClick={() => onSetSort(opt.value)}
                      >
                        {opt.label} {sort === opt.value ? "✓" : ""}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuGroup>
                )}
                {onSetPinnedOnly && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuItem
                        onClick={() => onSetPinnedOnly(!pinnedOnly)}
                      >
                        Pinned only {pinnedOnly ? "✓" : ""}
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                  </>
                )}
                {onSetVisibility && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>Show</DropdownMenuLabel>
                      {VISIBILITY_OPTIONS.map((opt) => {
                        // undefined / empty = all visibilities are shown.
                        const all = !visibility || visibility.length === 0;
                        const active = all || visibility.includes(opt.value);
                        return (
                          <DropdownMenuItem
                            key={opt.value}
                            onClick={() => toggleVisibility(opt.value)}
                          >
                            {opt.label} {active ? "✓" : ""}
                          </DropdownMenuItem>
                        );
                      })}
                    </DropdownMenuGroup>
                  </>
                )}
                {onSetLimit && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>How many</DropdownMenuLabel>
                      {LIMIT_OPTIONS.map((opt) => (
                        <DropdownMenuItem
                          key={opt}
                          onClick={() => onSetLimit(opt)}
                        >
                          {opt} {limit === opt ? "✓" : ""}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuGroup>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
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
            {pinnedOnly
              ? "No pinned notes yet."
              : "No notes yet — capture a thought."}
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
