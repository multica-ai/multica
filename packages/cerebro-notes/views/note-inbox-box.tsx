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
import {
  NotebookPen,
  ExternalLink,
  Plus,
  Settings2,
  X,
  ChevronDown,
  ChevronRight,
  Pencil,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { TextareaDictationMic } from "@multica/cerebro-dictation";
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

// Applied as an inline `!important` font-size in NoteEditor. base.css forces
// `font-size: 16px !important` on every <textarea> below 768px / on coarse
// pointers (to dodge iOS focus-zoom); a plain inline style loses to it, so on
// mobile the size selector did nothing. Inline `!important` is the only
// declaration that outranks the global rule on every engine (FIR-1487).
// Sizes: XS=8px, +2px per step.
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

/** FIR-1791 — how many editor lines stay visible before the box scrolls. */
const VISIBLE_LINES_OPTIONS = [4, 6, 8, 12, 20];
const DEFAULT_VISIBLE_LINES = 6;

export interface NoteInboxBoxProps {
  title?: string;
  /** The note embedded in this block. undefined = nothing picked yet. */
  noteId?: string;
  /** Persist the picked/created/detached note id into the section config. */
  onSetNoteId: (id: string | null) => void;
  /** FIR-1791 — rename the block. null / empty clears the custom name. */
  onSetTitle?: (title: string | null) => void;
  onRemove: () => void;
  /** Drag handle injected by the dynamic inbox's sortable wrapper. */
  dragHandle?: ReactNode;
  /** FIR-1487 — editor font size. Default "sm" (matches inbox text). */
  fontSize?: NoteFontSize;
  /** FIR-1487 — persist the font size choice into the section config. */
  onSetFontSize?: (size: NoteFontSize) => void;
  /** FIR-1791 — visible editor lines before the box scrolls. Default 6. */
  visibleLines?: number;
  /** FIR-1791 — persist the visible-lines choice into the section config. */
  onSetVisibleLines?: (lines: number) => void;
  /** FIR-1791 — can the block be folded open/closed? Default true. */
  collapsible?: boolean;
  /** FIR-1791 — does the block start folded closed? Default false (open). */
  defaultCollapsed?: boolean;
  /** FIR-1791 — persist the foldable choice into the section config. */
  onSetCollapsible?: (value: boolean) => void;
  /** FIR-1791 — persist the start-collapsed choice into the section config. */
  onSetDefaultCollapsed?: (value: boolean) => void;
}

export function NoteInboxBox({
  title,
  noteId,
  onSetNoteId,
  onSetTitle,
  onRemove,
  dragHandle,
  fontSize,
  onSetFontSize,
  visibleLines,
  onSetVisibleLines,
  collapsible: collapsibleProp,
  defaultCollapsed,
  onSetCollapsible,
  onSetDefaultCollapsed,
}: NoteInboxBoxProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const createNote = useCreateNote();

  const { data: note, isLoading } = useQuery({
    ...noteDetailOptions(wsId, noteId ?? ""),
    enabled: Boolean(wsId && noteId),
  });

  // FIR-1791 #2 — foldable block. Collapsible unless explicitly turned off; the
  // initial fold state comes from defaultCollapsed, the live toggle is
  // ephemeral UI state (mirrors DynamicInboxSection).
  const collapsible = collapsibleProp !== false;
  const [collapsed, setCollapsed] = React.useState<boolean>(
    defaultCollapsed === true,
  );
  const isCollapsed = collapsible && collapsed;

  // FIR-1791 #3 — inline rename of the block name.
  const [editing, setEditing] = React.useState(false);
  const commitRename = (value: string) => {
    const trimmed = value.trim();
    onSetTitle?.(trimmed ? trimmed : null);
    setEditing(false);
  };

  const handleCreate = async () => {
    const created = await createNote.mutateAsync({ visibility: "private" });
    if (created) onSetNoteId(created.id);
  };

  const openFull = () => {
    if (noteId) push(`${paths.notes()}?note=${encodeURIComponent(noteId)}`);
    else push(paths.notes());
  };

  const displayName = title?.trim() || (note ? firstLineTitle(note) : "Quick note");

  const header = (
    <header className="flex items-center gap-2 border-b border-border px-3 py-2">
      {dragHandle}
      {collapsible && (
        <button
          type="button"
          className="-ml-1 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
          onClick={() => setCollapsed((c) => !c)}
          title={isCollapsed ? "Expand" : "Collapse"}
          aria-label={isCollapsed ? "Expand" : "Collapse"}
        >
          {isCollapsed ? (
            <ChevronRight className="size-3.5" />
          ) : (
            <ChevronDown className="size-3.5" />
          )}
        </button>
      )}
      <NotebookPen className="size-3.5 shrink-0 text-muted-foreground" />
      {editing && onSetTitle ? (
        <input
          autoFocus
          defaultValue={title ?? ""}
          placeholder={note ? firstLineTitle(note) : "Quick note"}
          onBlur={(e) => commitRename(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") commitRename(e.currentTarget.value);
            else if (e.key === "Escape") setEditing(false);
          }}
          className="w-40 rounded border border-primary bg-transparent px-1 py-0.5 text-xs font-bold uppercase tracking-wide text-foreground outline-none"
          aria-label="Block name"
        />
      ) : (
        <button
          type="button"
          onDoubleClick={() => onSetTitle && setEditing(true)}
          title={onSetTitle ? "Double-click to rename" : undefined}
          className="truncate text-left text-xs font-bold uppercase tracking-wide text-muted-foreground hover:text-foreground"
        >
          {displayName}
        </button>
      )}
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
          onRename={onSetTitle ? () => setEditing(true) : undefined}
          onResetName={onSetTitle && title ? () => onSetTitle(null) : undefined}
          visibleLines={visibleLines}
          onSetVisibleLines={onSetVisibleLines}
          collapsible={collapsibleProp}
          defaultCollapsed={defaultCollapsed}
          onSetCollapsible={onSetCollapsible}
          onSetDefaultCollapsed={onSetDefaultCollapsed}
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
    bodySlot = (
      <NoteEditor note={note} fontSize={fontSize} visibleLines={visibleLines} />
    );
  }

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card">
      {header}
      {!isCollapsed && bodySlot}
    </section>
  );
}

/** Inline, debounced-autosave body editor for the embedded note. */
function NoteEditor({
  note,
  fontSize,
  visibleLines,
}: {
  note: Note;
  fontSize?: NoteFontSize;
  visibleLines?: number;
}) {
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

  // FIR-1487 — re-apply the chosen size as an inline `!important` font-size on
  // the textarea so it beats base.css's global `textarea { font-size:16px
  // !important }` mobile rule. React's `style` prop can't express `!important`,
  // and the Textarea component doesn't forward refs, so reach the node through
  // the wrapper. Verified on WebKit/iOS: without this the size stays 16px.
  const wrapRef = React.useRef<HTMLDivElement>(null);
  const taRef = React.useRef<HTMLTextAreaElement>(null);
  React.useEffect(() => {
    wrapRef.current
      ?.querySelector("textarea")
      ?.style.setProperty("font-size", FONT_SIZE_PX[fontSize ?? "sm"], "important");
  }, [fontSize]);

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

  // FIR-1791 — render a fixed number of visible lines and scroll past it.
  // `field-sizing-fixed` overrides the Textarea's default `field-sizing-content`
  // (which auto-grows with content); `rows` then sets the visible height in
  // lines, and overflow scrolls. `min-h-0` drops the base `min-h-16` floor so
  // small line counts aren't forced taller.
  const rows = Math.max(1, visibleLines ?? DEFAULT_VISIBLE_LINES);
  return (
    <div ref={wrapRef} className="relative p-2">
      <Textarea
        ref={taRef}
        rows={rows}
        value={body}
        onChange={(e) => onChange(e.target.value)}
        onBlur={flush}
        placeholder="Write a note… (the first line becomes the title)"
        className="field-sizing-fixed min-h-0 resize-none overflow-y-auto border-0 bg-transparent shadow-none focus-visible:ring-0"
      />
      <TextareaDictationMic textareaRef={taRef} className="absolute bottom-3 right-3 z-10" />
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
  onRename,
  onResetName,
  visibleLines,
  onSetVisibleLines,
  collapsible,
  defaultCollapsed,
  onSetCollapsible,
  onSetDefaultCollapsed,
}: {
  wsId: string;
  activeId?: string;
  onPick: (id: string) => void;
  onDetach: () => void;
  onCreate: () => void;
  fontSize?: NoteFontSize;
  onSetFontSize?: (size: NoteFontSize) => void;
  onRename?: () => void;
  onResetName?: () => void;
  visibleLines?: number;
  onSetVisibleLines?: (lines: number) => void;
  collapsible?: boolean;
  defaultCollapsed?: boolean;
  onSetCollapsible?: (value: boolean) => void;
  onSetDefaultCollapsed?: (value: boolean) => void;
}) {
  const { data: notes = [] } = useQuery(notesListOptions(wsId, { limit: 20 }));
  const activeFontSize = fontSize ?? "sm";
  const activeVisibleLines = visibleLines ?? DEFAULT_VISIBLE_LINES;
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
        {onRename && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem onClick={onRename}>
                <Pencil className="size-3.5" /> Rename block
              </DropdownMenuItem>
              {onResetName && (
                <DropdownMenuItem onClick={onResetName}>
                  Reset name
                </DropdownMenuItem>
              )}
            </DropdownMenuGroup>
          </>
        )}
        {(onSetCollapsible || onSetDefaultCollapsed) && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>Folding</DropdownMenuLabel>
              {onSetCollapsible && (
                <DropdownMenuItem
                  onClick={() => onSetCollapsible(collapsible === false)}
                >
                  Foldable {collapsible !== false ? "✓" : ""}
                </DropdownMenuItem>
              )}
              {onSetDefaultCollapsed && (
                <DropdownMenuItem
                  onClick={() => onSetDefaultCollapsed(!defaultCollapsed)}
                >
                  Start closed {defaultCollapsed ? "✓" : ""}
                </DropdownMenuItem>
              )}
            </DropdownMenuGroup>
          </>
        )}
        {onSetVisibleLines && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>Visible lines</DropdownMenuLabel>
              {VISIBLE_LINES_OPTIONS.map((lines) => (
                <DropdownMenuItem
                  key={lines}
                  onClick={() => onSetVisibleLines(lines)}
                >
                  <span className="flex w-full items-center justify-between">
                    <span>{lines} lines</span>
                    {lines === activeVisibleLines && (
                      <span className="text-muted-foreground">✓</span>
                    )}
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </>
        )}
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
