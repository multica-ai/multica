"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Plus,
  Search,
  Pin,
  Trash2,
  Lock,
  Users,
  Globe,
  NotebookPen,
  ChevronLeft,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuCheckboxItem,
  DropdownMenuGroup,
} from "@multica/ui/components/ui/dropdown-menu";
import { History, MessageSquare } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import {
  ContentEditor,
  type ContentEditorRef,
  ReadonlyContent,
} from "@multica/views/editor";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  notesListOptions,
  useCreateNote,
  useUpdateNote,
  useDeleteNote,
  useSetNotePin,
  useSetNoteVisibility,
  useNoteEditLock,
  firstLineTitle,
  VISIBILITY_LABELS,
} from "../core";
import type { Note, NoteVisibility } from "../core";
import { NoteReferences } from "./note-references";
import { NoteLockBanner } from "./note-lock-banner";
import { NoteCommentsPanel } from "./note-comments-panel";
import { NoteVersionsDialog } from "./note-versions-dialog";

const VIS_ICON: Record<NoteVisibility, React.ReactNode> = {
  private: <Lock className="size-3" />,
  shared: <Users className="size-3" />,
  workspace: <Globe className="size-3" />,
};

// NotesPage is the fast Notes surface: a search + capture header, a pinned-first
// list on the left, and the selected note's editor on the right. Private by
// default; visibility is changed per note. Built on top of Documents — a note
// is an artifact(kind=note) plus owner/visibility/pin state.
export function NotesPage({ initialNoteId }: { initialNoteId?: string | null }) {
  const wsId = useWorkspaceId();
  const [search, setSearch] = React.useState("");
  const [selectedId, setSelectedId] = React.useState<string | null>(
    initialNoteId ?? null,
  );

  const { data: notes = [] } = useQuery(
    notesListOptions(wsId, { q: search.trim() || undefined }),
  );

  const createNote = useCreateNote();

  const selected = React.useMemo(
    () => notes.find((n) => n.id === selectedId) ?? null,
    [notes, selectedId],
  );

  const pinned = notes.filter((n) => n.pinned);
  const rest = notes.filter((n) => !n.pinned);

  async function handleNew() {
    const note = await createNote.mutateAsync({ visibility: "private" });
    if (note) setSelectedId(note.id);
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4 sm:gap-3 sm:px-5">
        <MobileSidebarTrigger className="mr-0" />
        <NotebookPen className="hidden size-5 shrink-0 text-muted-foreground sm:block" />
        <h1 className="text-sm font-semibold">Notes</h1>
        <span className="hidden text-xs text-muted-foreground md:inline">
          {notes.length} {notes.length === 1 ? "note" : "notes"} · private by
          default
        </span>
        <div className="ml-auto flex min-w-0 items-center gap-2">
          <div className="relative min-w-0 flex-1 sm:flex-none">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search notes…"
              className="h-8 w-full pl-8 sm:w-56"
            />
          </div>
          <Button
            size="sm"
            onClick={handleNew}
            disabled={createNote.isPending}
            className="shrink-0"
          >
            <Plus className="size-4" />
            <span className="hidden sm:inline">New note</span>
          </Button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        {/* List: full-width on mobile, fixed rail on desktop. Hidden on mobile
            once a note is open so the editor gets the full screen. */}
        <div
          className={cn(
            "w-full overflow-auto border-r lg:block lg:w-80 lg:shrink-0",
            selectedId ? "hidden" : "block",
          )}
        >
          {notes.length === 0 ? (
            <p className="p-6 text-sm text-muted-foreground">
              No notes yet. Tap “New note” to capture a thought.
            </p>
          ) : (
            <>
              {pinned.length > 0 && (
                <NoteListSection
                  label="📌 Pinned"
                  notes={pinned}
                  selectedId={selectedId}
                  onSelect={setSelectedId}
                />
              )}
              <NoteListSection
                label={pinned.length > 0 ? "Recent" : ""}
                notes={rest}
                selectedId={selectedId}
                onSelect={setSelectedId}
              />
            </>
          )}
        </div>

        {/* Editor: full-screen on mobile when a note is open, otherwise hidden
            on mobile (the list owns the screen). Always shown on desktop. */}
        <div
          className={cn(
            "min-w-0 flex-1 lg:flex",
            selected ? "flex" : "hidden lg:flex",
          )}
        >
          {selected ? (
            <NoteEditor
              key={selected.id}
              note={selected}
              wsId={wsId}
              onBack={() => setSelectedId(null)}
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center text-sm text-muted-foreground">
              Select a note, or tap “New note”.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function NoteListSection({
  label,
  notes,
  selectedId,
  onSelect,
}: {
  label: string;
  notes: Note[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  if (notes.length === 0) return null;
  return (
    <div>
      {label && (
        <div className="px-4 pb-1.5 pt-3 text-[11px] uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
      )}
      {notes.map((n) => (
        <button
          key={n.id}
          onClick={() => onSelect(n.id)}
          className={cn(
            "block w-full border-b px-4 py-2.5 text-left hover:bg-muted/50",
            selectedId === n.id && "bg-muted",
          )}
        >
          <div className="flex items-center gap-1.5 text-[13px] font-medium">
            {n.pinned && <Pin className="size-3 text-amber-500" />}
            <span className="truncate">{firstLineTitle(n)}</span>
          </div>
          <div className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
            {previewBody(n)}
          </div>
          <div className="mt-1.5 flex items-center gap-1 text-[11px] text-muted-foreground">
            {VIS_ICON[n.visibility]}
            <span>{VISIBILITY_LABELS[n.visibility]}</span>
          </div>
        </button>
      ))}
    </div>
  );
}

function NoteEditor({
  note,
  wsId,
  onBack,
}: {
  note: Note;
  wsId: string;
  onBack: () => void;
}) {
  const updateNote = useUpdateNote();
  const deleteNote = useDeleteNote();
  const setPin = useSetNotePin();
  const setVisibility = useSetNoteVisibility();

  // Wave 3 (TECH-3556): each surface is independently feature-flagged.
  const lockEnabled = useFeatureFlag("cerebro_note_lock");
  const commentsEnabled = useFeatureFlag("cerebro_note_comments");
  const versionsEnabled = useFeatureFlag("cerebro_note_versions");

  const [title, setTitle] = React.useState(note.title);
  const [sharedIds, setSharedIds] = React.useState<string[]>([]);
  const [showComments, setShowComments] = React.useState(false);
  const [showHistory, setShowHistory] = React.useState(false);
  const editorRef = React.useRef<ContentEditorRef>(null);
  const editorContainerRef = React.useRef<HTMLDivElement>(null);

  const myId = useAuthStore((s: { user: { id: string } | null }) => s.user?.id);
  const isOwner = note.owner_id === myId;

  // Edit lock: while this note is open and the flag is on, hold the lock and
  // heartbeat. If someone else holds it live, drop to read-only + offer takeover.
  const editLock = useNoteEditLock(note.id, lockEnabled);
  const readOnly = lockEnabled && editLock.blockedByOther;

  const { data: members = [] } = useQuery(memberListOptions(wsId));

  function saveTitle() {
    if (title !== note.title) updateNote.mutate({ id: note.id, title });
  }

  // The body is the shared rich editor (same as issue comments/descriptions):
  // typing "@" opens the identical mention picker, and inline mentions are
  // stored as markdown that round-trips through the editor. Auto-save on the
  // debounced update and again on blur so nothing is lost on navigate-away.
  function saveBody(markdown: string) {
    if (markdown !== note.body) updateNote.mutate({ id: note.id, body: markdown });
  }

  function applyVisibility(v: NoteVisibility) {
    setVisibility.mutate({
      id: note.id,
      visibility: v,
      sharedUserIds: v === "shared" ? sharedIds : undefined,
    });
  }

  function toggleShare(userId: string) {
    const next = sharedIds.includes(userId)
      ? sharedIds.filter((id) => id !== userId)
      : [...sharedIds, userId];
    setSharedIds(next);
    setVisibility.mutate({
      id: note.id,
      visibility: "shared",
      sharedUserIds: next,
    });
  }

  return (
    <div className="flex h-full min-h-0 w-full flex-col">
      {readOnly && editLock.lock && (
        <NoteLockBanner
          lock={editLock.lock}
          acquiring={editLock.acquiring}
          onTakeOver={editLock.takeOver}
        />
      )}
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5 sm:px-5">
        <Button
          size="sm"
          variant="ghost"
          className="-ml-2 shrink-0 lg:hidden"
          onClick={onBack}
        >
          <ChevronLeft className="size-4" /> Notes
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            nativeButton={false}
            render={
              <Badge variant="outline" className="cursor-pointer gap-1.5">
                {VIS_ICON[note.visibility]}
                {VISIBILITY_LABELS[note.visibility]}
              </Badge>
            }
          />
          <DropdownMenuContent align="start" className="w-60">
            <DropdownMenuRadioGroup
              value={note.visibility}
              onValueChange={(v) => applyVisibility(v as NoteVisibility)}
            >
              <DropdownMenuLabel>Who can see it</DropdownMenuLabel>
              <DropdownMenuRadioItem value="private">
                Only you
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="shared">
                Selected colleagues
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="workspace">
                Whole team
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
            {note.visibility === "shared" && members.length > 0 && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Share with</DropdownMenuLabel>
                  {members.map((m) => (
                    <DropdownMenuCheckboxItem
                      key={m.user_id}
                      checked={sharedIds.includes(m.user_id)}
                      onCheckedChange={() => toggleShare(m.user_id)}
                    >
                      {m.name}
                    </DropdownMenuCheckboxItem>
                  ))}
                </DropdownMenuGroup>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        <Button
          size="sm"
          variant={note.pinned ? "secondary" : "ghost"}
          onClick={() => setPin.mutate({ id: note.id, pinned: !note.pinned })}
        >
          <Pin className="size-4" />
          <span className="hidden sm:inline">
            {note.pinned ? "Pinned" : "Pin"}
          </span>
        </Button>

        {versionsEnabled && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setShowHistory(true)}
          >
            <History className="size-4" />
            <span className="hidden sm:inline">History</span>
          </Button>
        )}

        {commentsEnabled && (
          <Button
            size="sm"
            variant={showComments ? "secondary" : "ghost"}
            onClick={() => setShowComments((v) => !v)}
          >
            <MessageSquare className="size-4" />
            <span className="hidden sm:inline">Comments</span>
          </Button>
        )}

        <Button
          size="sm"
          variant="ghost"
          className="ml-auto text-destructive"
          onClick={() => deleteNote.mutate(note.id)}
        >
          <Trash2 className="size-4" />
          <span className="hidden sm:inline">Delete</span>
        </Button>
      </div>

      <div className="flex min-h-0 flex-1">
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-6">
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onBlur={saveTitle}
            disabled={readOnly}
            placeholder="Title (optional — first line becomes the title)"
            className="w-full bg-transparent text-2xl font-bold outline-none placeholder:text-muted-foreground/50 disabled:opacity-70"
          />

          <NoteReferences noteId={note.id} />

          {/* Body uses the SAME rich editor as issue comments + descriptions, so
              "@" behaves identically (people, agents, issues, …) inline. When
              the note is locked by someone else we render it read-only instead. */}
          {readOnly ? (
            <ReadonlyContent content={note.body} className="min-h-[50vh] flex-1" />
          ) : (
            <div ref={editorContainerRef} className="flex min-h-0 flex-1 flex-col">
              <ContentEditor
                ref={editorRef}
                defaultValue={note.body}
                onUpdate={saveBody}
                onBlur={() => saveBody(editorRef.current?.getMarkdown() ?? "")}
                placeholder="Just start writing… (type “@” to mention a person, agent or issue)"
                className="min-h-[50vh] flex-1"
              />
            </div>
          )}
        </div>

        {commentsEnabled && showComments && (
          <div className="hidden w-80 shrink-0 border-l sm:block">
            <NoteCommentsPanel
              noteId={note.id}
              noteBody={note.body}
              isOwner={isOwner}
              editorContainerRef={editorContainerRef}
              onClose={() => setShowComments(false)}
            />
          </div>
        )}
      </div>

      {versionsEnabled && (
        <NoteVersionsDialog
          noteId={note.id}
          open={showHistory}
          onOpenChange={setShowHistory}
        />
      )}
    </div>
  );
}

function previewBody(note: Note): string {
  const lines = note.body.split("\n").filter((l) => l.trim().length > 0);
  // Drop the first line when it doubled as the title, so the preview shows the
  // body rather than repeating the heading.
  const startsWithTitle =
    !note.title.trim() && lines.length > 0 ? 1 : 0;
  return lines.slice(startsWithTitle).join(" ") || "Empty note";
}
