"use client";

// CEREBRO-PATCH(cerebro-note-inbox-box): TECH-3690 (Jesper) — a "Quick note"
// block for the dynamic inbox: a single, fixed note you can read and edit
// inline without leaving the inbox. Distinct from the Notes *list* box
// (notes-inbox-box.tsx): this embeds ONE note with a lightweight textarea
// editor that autosaves (debounced + on blur). Rich editing still lives on the
// full Notes surface — the "Open" button deep-links there. Self-contained:
// owns its own data + chrome so the dynamic inbox only has to mount it.
import * as React from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { NotebookPen, ExternalLink, Plus, Settings2, X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { Textarea } from "@multica/ui/components/ui/textarea";
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
  firstLineTitle,
  noteDetailOptions,
  notesListOptions,
  useCreateNote,
  useUpdateNote,
  type Note,
} from "../core";

/** Debounce window for the inline autosave. */
const SAVE_DEBOUNCE_MS = 600;

/** FIR-1487 — available font sizes for the Quick note editor. */
export type NoteFontSize = "xs" | "sm" | "base" | "lg";

// Inline style bypasses the Textarea component's `text-base md:text-sm`
// defaults entirely — no breakpoint dependency, works on all viewport sizes.
// Sizes: XS=8px, +2px per step (FIR-1487).
const FONT_SIZE_PX: Record<NoteFontSize, string> = {
  xs: "8px",
  sm: "10px",
  base: "12px",
  lg: "14px",
};

const FONT_SIZE_LABELS: Record<NoteFontSize, string> = {
  xs: "X-Small",
  sm: "Small",
  base: "Medium",
  lg: "Large",
};

export interface NoteInboxBoxProps {
  title?: string;
  /** The note embedded in this block. undefined = nothing picked yet. */
  noteId?: string;
  /** Persist the picked/created/detached note id into the section config. */
  onSetNoteId: (id: string | null) => void;
  onRemove: () => void;
  /** Drag handle injected by the dynamic inbox's sortable wrapper. */
  dragHandle?: ReactNode;
  /** FIR-1487 — editor font size. Default "sm" (matches inbox text). */
  fontSize?: NoteFontSize;
  /** FIR-1487 — persist the font size choice into the section config. */
  onSetFontSize?: (size: NoteFontSize) => void;
}

export function NoteInboxBox({
  title,
  noteId,
  onSetNoteId,
  onRemove,
  dragHandle,
  fontSize,
  onSetFontSize,
}: NoteInboxBoxProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const createNote = useCreateNote();

  const { data: note, isLoading } = useQuery({
    ...noteDetailOptions(wsId, noteId ?? ""),
    enabled: Boolean(wsId && noteId),
  });

  const handleCreate = async () => {
    const created = await createNote.mutateAsync({ visibility: "private" });
    if (created) onSetNoteId(created.id);
  };

  const openFull = () => {
    if (noteId) push(`${paths.notes()}?note=${encodeURIComponent(noteId)}`);
    else push(paths.notes());
  };

  const header = (
    <header className="flex items-center gap-2 border-b border-border px-3 py-2">
      {dragHandle}
      <NotebookPen className="size-3.5 text-muted-foreground" />
      <span className="truncate text-xs font-bold uppercase tracking-wide text-muted-foreground">
        {title?.trim() || (note ? firstLineTitle(note) : "Quick note")}
      </span>
      <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
        {noteId && (
          <button
            type="button"
            className="rounded p-1 hover:bg-muted"
            onClick={openFull}
            title="Open full note"
          >
            <ExternalLink className="size-3.5" />
          </button>
        )}
        <NotePicker
          wsId={wsId}
          activeId={noteId}
          onPick={onSetNoteId}
          onDetach={() => onSetNoteId(null)}
          onCreate={handleCreate}
          fontSize={fontSize}
          onSetFontSize={onSetFontSize}
        />
        <button
          type="button"
          className="rounded p-1 hover:bg-muted"
          onClick={onRemove}
          title="Remove block"
        >
          <X className="size-3.5" />
        </button>
      </div>
    </header>
  );

  let bodySlot: ReactNode;
  if (!noteId) {
    bodySlot = (
      <div className="flex flex-col gap-2 p-3">
        <p className="text-xs text-muted-foreground">
          Pin a note here to read and edit it without leaving the inbox.
        </p>
        <button
          type="button"
          onClick={handleCreate}
          disabled={createNote.isPending}
          className="flex w-fit items-center gap-1 rounded border border-border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
        >
          <Plus className="size-3.5" />
          New quick note
        </button>
      </div>
    );
  } else if (isLoading) {
    bodySlot = (
      <div className="p-3 text-xs text-muted-foreground">Loading note…</div>
    );
  } else if (!note) {
    // The pinned note was deleted or is no longer visible — let the user pick
    // another instead of showing a dead block.
    bodySlot = (
      <div className="flex flex-col gap-2 p-3">
        <p className="text-xs text-muted-foreground">
          This note is no longer available.
        </p>
        <button
          type="button"
          onClick={() => onSetNoteId(null)}
          className="w-fit rounded border border-border px-2 py-1 text-xs hover:bg-muted"
        >
          Pick another note
        </button>
      </div>
    );
  } else {
    bodySlot = <NoteEditor note={note} fontSize={fontSize} />;
  }

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card">
      {header}
      {bodySlot}
    </section>
  );
}

/** Inline, debounced-autosave body editor for the embedded note. */
function NoteEditor({ note, fontSize }: { note: Note; fontSize?: NoteFontSize }) {
  const updateNote = useUpdateNote();
  const [body, setBody] = React.useState(note.body);
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  // Latest body for the unmount flush, without re-arming the effect on typing.
  const bodyRef = React.useRef(body);
  bodyRef.current = body;

  // Reseed when a different note is embedded (note id changes), not on every
  // server echo of the same note — otherwise an in-flight save would clobber
  // what the user is typing.
  React.useEffect(() => {
    setBody(note.body);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [note.id]);

  const save = React.useCallback(
    (next: string) => {
      if (next !== note.body) updateNote.mutate({ id: note.id, body: next });
    },
    [note.id, note.body, updateNote],
  );

  const onChange = (next: string) => {
    setBody(next);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => save(next), SAVE_DEBOUNCE_MS);
  };

  const flush = () => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    save(bodyRef.current);
  };

  // Flush any pending edit on unmount so nothing is lost on navigate-away.
  React.useEffect(() => {
    return () => {
      if (timer.current) {
        clearTimeout(timer.current);
        save(bodyRef.current);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [note.id]);

  return (
    <div className="p-2">
      <Textarea
        value={body}
        onChange={(e) => onChange(e.target.value)}
        onBlur={flush}
        placeholder="Write a note… (the first line becomes the title)"
        style={{ fontSize: FONT_SIZE_PX[fontSize ?? "sm"] }}
        className="min-h-28 resize-y border-0 bg-transparent shadow-none focus-visible:ring-0"
      />
    </div>
  );
}

const FONT_SIZE_OPTIONS: NoteFontSize[] = ["xs", "sm", "base", "lg"];

/** Dropdown to create / pick / detach the note this block shows, and to
 *  control per-block settings like font size. */
function NotePicker({
  wsId,
  activeId,
  onPick,
  onDetach,
  onCreate,
  fontSize,
  onSetFontSize,
}: {
  wsId: string;
  activeId?: string;
  onPick: (id: string) => void;
  onDetach: () => void;
  onCreate: () => void;
  fontSize?: NoteFontSize;
  onSetFontSize?: (size: NoteFontSize) => void;
}) {
  const { data: notes = [] } = useQuery(notesListOptions(wsId, { limit: 20 }));
  const activeFontSize = fontSize ?? "sm";
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className="rounded p-1 hover:bg-muted"
            title="Choose note"
          />
        }
      >
        <Settings2 className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuGroup>
          <DropdownMenuItem onClick={onCreate}>
            <Plus className="size-3.5" /> New quick note
          </DropdownMenuItem>
          {activeId && (
            <DropdownMenuItem onClick={onDetach}>Detach note</DropdownMenuItem>
          )}
        </DropdownMenuGroup>
        {onSetFontSize && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>Font size</DropdownMenuLabel>
              {FONT_SIZE_OPTIONS.map((size) => (
                <DropdownMenuItem
                  key={size}
                  onClick={() => onSetFontSize(size)}
                >
                  <span className="flex w-full items-center justify-between">
                    <span>{FONT_SIZE_LABELS[size]}</span>
                    {size === activeFontSize && (
                      <span className="text-muted-foreground">✓</span>
                    )}
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </>
        )}
        {notes.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>Pin an existing note</DropdownMenuLabel>
              {notes.map((n: Note) => (
                <DropdownMenuItem key={n.id} onClick={() => onPick(n.id)}>
                  <span className="truncate">
                    {firstLineTitle(n)} {n.id === activeId ? "✓" : ""}
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
