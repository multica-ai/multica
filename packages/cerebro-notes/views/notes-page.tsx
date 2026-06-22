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
  ChevronDown,
  ChevronRight,
  Repeat,
  Folder,
  FolderPlus,
  Link2,
  ListPlus,
  ExternalLink,
  User,
  Pencil,
  Replace,
  Share2,
  Check,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@multica/ui/components/ui/sheet";
import {
  NoteTypesPanel,
  DocumentToolsSidebar,
  EditableTitle,
  EditorActionsMenu,
  EntityMetaHeader,
  FindReplaceBar,
  FolderAccessControl,
} from "@multica/cerebro-artifacts/views/components";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuGroup,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { History, MessageSquare } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
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
import type { Editor } from "@tiptap/react";
import {
  notesListOptions,
  useCreateNote,
  useUpdateNote,
  useDeleteNote,
  useSetNotePin,
  useSetNoteVisibility,
  useNoteEditLock,
  useNoteComments,
  useSetNoteFolder,
  firstLineTitle,
  VISIBILITY_LABELS,
} from "../core";
import type { Note, NoteVisibility } from "../core";
import {
  artifactFoldersOptions,
  useCreateArtifactFolder,
  useUpdateArtifactFolder,
} from "@multica/cerebro-artifacts/core";
import type { ArtifactFolder, MemberWithUser } from "@multica/core/types";
import { NoteReferences, NoteAddReferenceDialog } from "./note-references";
import { NoteCreateIssueDialog } from "./note-create-issue-dialog";
import { NoteFolderCreateDialog } from "./note-folder-create-dialog";
import { NoteLockBanner } from "./note-lock-banner";
import { NoteCommentsPanel } from "./note-comments-panel";
import { NoteVersionsDialog } from "./note-versions-dialog";
import { useCommentAnchors } from "./use-comment-anchors";
import {
  DRAFT_ANCHOR_ID,
  type CommentAnchor,
} from "./comment-anchor-plugin";

// drag-and-drop payload type for moving a note row onto a folder.
const NOTE_DND_TYPE = "application/x-note-id";

const VIS_ICON: Record<NoteVisibility, React.ReactNode> = {
  private: <Lock className="size-3" />,
  shared: <Users className="size-3" />,
  workspace: <Globe className="size-3" />,
};

// A note carries an owner_id but no owner name (the wire shape is lightweight),
// so resolve the display name from the workspace member list (FIR-1460). Used by
// the list rows and the editor so you can always see who owns a note.
type OwnerInfo = Pick<MemberWithUser, "user_id" | "name">;

function ownerName(
  ownerId: string,
  myId: string | undefined,
  members: OwnerInfo[],
): string {
  if (ownerId && ownerId === myId) return "You";
  const m = members.find((x) => x.user_id === ownerId);
  return m?.name?.trim() || "Unknown";
}

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
  // Recurring notes ("Note types") used to live only inside the Documents file
  // manager, so they were hard to find (TECH-3637). Surface the same panel here.
  const noteTypesEnabled = useFeatureFlag("cerebro_note_types");
  const [showRecurring, setShowRecurring] = React.useState(false);
  // Folder creation from the "New note" split-button menu (TECH-3690), so it
  // works even when the folder rail is off-screen (mobile, note open).
  const [creatingFolder, setCreatingFolder] = React.useState(false);
  // Folders (TECH-3637). Notes keep their own folder namespace, separate from
  // Documents (kind: "note"). `folderId` is the folder we're currently inside:
  // null = the root, where loose (unfiled) notes and top-level folders live.
  // Drilling into a folder scopes the list to that folder + shows its
  // subfolders, with a breadcrumb back to the root.
  const [folderId, setFolderId] = React.useState<string | null>(null);
  const { data: folders = [] } = useQuery(
    artifactFoldersOptions(wsId, { kind: "note" }),
  );
  const setNoteFolder = useSetNoteFolder();
  const createFolder = useCreateArtifactFolder();

  const { data: notes = [] } = useQuery(
    notesListOptions(wsId, { q: search.trim() || undefined }),
  );
  // Owner display (FIR-1460): resolve owner_id → name for the list + editor.
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const user = useAuthStore((s: { user: { id: string; name: string } | null }) => s.user);
  const myId = user?.id;

  const createNote = useCreateNote();

  const selected = React.useMemo(
    () => notes.find((n) => n.id === selectedId) ?? null,
    [notes, selectedId],
  );

  const noteFolderIds = React.useMemo(
    () => new Set(folders.map((f) => f.id)),
    [folders],
  );
  const searching = search.trim().length > 0;
  const visibleNotes = React.useMemo(() => {
    // While searching, show flat matches across every folder. Otherwise the
    // list is scoped to the current folder (root shows the loose notes).
    if (searching) return notes;
    // Root: loose notes, plus any whose folder_id points outside the note
    // folder tree — e.g. a note filed before notes got their own folders
    // (TECH-3637). Treat that as unfiled so the note never disappears.
    if (folderId === null)
      return notes.filter(
        (n) => !n.folder_id || !noteFolderIds.has(n.folder_id),
      );
    return notes.filter((n) => n.folder_id === folderId);
  }, [notes, folderId, searching, noteFolderIds]);

  const pinned = visibleNotes.filter((n) => n.pinned);
  const rest = visibleNotes.filter((n) => !n.pinned);

  function dropNoteInFolder(noteId: string, folderId: string | null) {
    setNoteFolder.mutate({ id: noteId, folderId });
  }

  // ensurePersonalFolder returns the id of the folder named after the current
  // user, creating it once if it doesn't exist yet (FIR-1460, request 4). Used
  // as the default home for new notes captured from the root.
  async function ensurePersonalFolder(): Promise<string | null> {
    const name = user?.name?.trim();
    if (!name) return null;
    const existing = folders.find(
      (f) => f.name.trim().toLowerCase() === name.toLowerCase(),
    );
    if (existing) return existing.id;
    const created = await createFolder.mutateAsync({
      name,
      kind: "note",
      parent_id: null,
    });
    return created?.id ?? null;
  }

  async function handleNew() {
    // New notes are private ("Only you") by default, and — when captured from
    // the root — land in a folder named after the user (FIR-1460, request 4).
    // Inside a specific folder, the note stays there.
    let targetFolder = folderId;
    if (targetFolder === null) {
      targetFolder = await ensurePersonalFolder();
    }
    const note = await createNote.mutateAsync({
      visibility: "private",
      folder_id: targetFolder,
    });
    if (note) {
      if (folderId === null && targetFolder) setFolderId(targetFolder);
      setSelectedId(note.id);
    }
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
          {/* Search lives in the folder/list rail now (TECH-3637), not the top
              bar. The "New note" split button always carries a caret menu so a
              new folder can be made even when the folder rail is hidden (mobile,
              note open) — TECH-3690 — plus recurring notes when enabled. */}
          <div className="flex shrink-0 items-stretch">
            <Button
              size="sm"
              onClick={handleNew}
              disabled={createNote.isPending}
              className="rounded-r-none"
            >
              <Plus className="size-4" />
              {/* Label is always shown — the bare "+" on mobile was unclear
                  (TECH-3690). */}
              <span>New note</span>
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    size="sm"
                    aria-label="More create options"
                    className="rounded-l-none border-l border-primary-foreground/20 px-1.5"
                  >
                    <ChevronDown className="size-4" />
                  </Button>
                }
              />
              <DropdownMenuContent align="end" className="w-52">
                <DropdownMenuGroup>
                  <DropdownMenuItem onClick={handleNew}>
                    <Plus className="size-4" /> New note
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setCreatingFolder(true)}>
                    <FolderPlus className="size-4" /> New folder
                  </DropdownMenuItem>
                </DropdownMenuGroup>
                {noteTypesEnabled && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>Recurring</DropdownMenuLabel>
                      <DropdownMenuItem onClick={() => setShowRecurring(true)}>
                        <Repeat className="size-4" /> Start or edit recurring…
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
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
          <FolderRail
            folders={folders}
            notes={notes}
            current={folderId}
            searching={searching}
            search={search}
            onSearch={setSearch}
            onNavigate={setFolderId}
            onDropNote={dropNoteInFolder}
          />
          {notes.length === 0 ? (
            <p className="p-6 text-sm text-muted-foreground">
              No notes yet. Tap “New note” to capture a thought.
            </p>
          ) : visibleNotes.length === 0 ? (
            <p className="p-6 text-sm text-muted-foreground">
              {searching
                ? "No notes match your search."
                : "No notes here yet. Drag a note in, or tap “New note”."}
            </p>
          ) : (
            <>
              {pinned.length > 0 && (
                <NoteListSection
                  label="📌 Pinned"
                  notes={pinned}
                  selectedId={selectedId}
                  onSelect={setSelectedId}
                  myId={myId}
                  members={members}
                />
              )}
              <NoteListSection
                label={pinned.length > 0 ? "Recent" : ""}
                notes={rest}
                selectedId={selectedId}
                onSelect={setSelectedId}
                myId={myId}
                members={members}
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

      <NoteFolderCreateDialog
        open={creatingFolder}
        onOpenChange={setCreatingFolder}
        parentId={folderId}
        onCreated={(folder) => setFolderId(folder.id)}
      />

      {noteTypesEnabled && (
        <Sheet open={showRecurring} onOpenChange={setShowRecurring}>
          <SheetContent className="overflow-y-auto data-[side=right]:w-[94vw] data-[side=right]:sm:max-w-xl">
            <SheetHeader>
              <SheetTitle>Recurring notes</SheetTitle>
              <SheetDescription>
                Templates for notes that recur on a schedule — e.g. a weekly
                business review. Create one here, then it appears on its cadence.
              </SheetDescription>
            </SheetHeader>
            <div className="px-4 pb-6">
              <NoteTypesPanel
                onOpenNote={(id) => {
                  // FIR-1460: open the just-started recurring note right here on
                  // the Notes surface and close the sheet so it isn't hidden.
                  setShowRecurring(false);
                  setSelectedId(id);
                }}
              />
            </div>
          </SheetContent>
        </Sheet>
      )}
    </div>
  );
}

// FolderRail (TECH-3637): the navigable folder menu at the top of the notes
// rail. It holds the note search, a breadcrumb of where you are, and the
// subfolders of the current folder. Clicking a folder drills in (the list
// scopes to it); the breadcrumb walks back out. Each folder row is a drop
// target so a note can be dragged into it. "New folder" creates a folder inside
// the current one, so folders nest.
function FolderRail({
  folders,
  notes,
  current,
  searching,
  search,
  onSearch,
  onNavigate,
  onDropNote,
}: {
  folders: ArtifactFolder[];
  notes: Note[];
  current: string | null;
  searching: boolean;
  search: string;
  onSearch: (v: string) => void;
  onNavigate: (id: string | null) => void;
  onDropNote: (noteId: string, folderId: string | null) => void;
}) {
  const createFolder = useCreateArtifactFolder();
  const renameFolder = useUpdateArtifactFolder();
  const [adding, setAdding] = React.useState(false);
  const [name, setName] = React.useState("");
  const [dragOver, setDragOver] = React.useState<string | null>(null);
  // Inline folder rename (FIR-1460, request 3).
  const [renamingId, setRenamingId] = React.useState<string | null>(null);
  const [renameName, setRenameName] = React.useState("");

  function startRename(f: ArtifactFolder) {
    setRenamingId(f.id);
    setRenameName(f.name);
  }
  function submitRename() {
    const trimmed = renameName.trim();
    if (!trimmed || !renamingId) return;
    renameFolder.mutate(
      { id: renamingId, data: { name: trimmed } },
      {
        onSuccess: () => {
          setRenamingId(null);
          setRenameName("");
        },
      },
    );
  }

  const byId = React.useMemo(() => {
    const m = new Map<string, ArtifactFolder>();
    folders.forEach((f) => m.set(f.id, f));
    return m;
  }, [folders]);

  // Breadcrumb: walk the parent chain from the current folder up to the root.
  const trail = React.useMemo(() => {
    const out: ArtifactFolder[] = [];
    let cur = current ? byId.get(current) : undefined;
    const guard = new Set<string>();
    while (cur && !guard.has(cur.id)) {
      guard.add(cur.id);
      out.unshift(cur);
      cur = cur.parent_id ? byId.get(cur.parent_id) : undefined;
    }
    return out;
  }, [byId, current]);

  const childFolders = folders.filter((f) => (f.parent_id ?? null) === current);
  const countFor = (id: string | null) =>
    notes.filter((n) => (n.folder_id ?? null) === id).length;

  function submitFolder() {
    const trimmed = name.trim();
    if (!trimmed) return;
    createFolder.mutate(
      { name: trimmed, parent_id: current, kind: "note" },
      { onSuccess: () => { setName(""); setAdding(false); } },
    );
  }

  function dropHandlers(key: string, folderId: string | null) {
    return {
      onDragOver: (e: React.DragEvent) => {
        if (e.dataTransfer.types.includes(NOTE_DND_TYPE)) {
          e.preventDefault();
          e.dataTransfer.dropEffect = "move";
          setDragOver(key);
        }
      },
      onDragLeave: () => setDragOver((k) => (k === key ? null : k)),
      onDrop: (e: React.DragEvent) => {
        e.preventDefault();
        setDragOver(null);
        const noteId = e.dataTransfer.getData(NOTE_DND_TYPE);
        if (noteId) onDropNote(noteId, folderId);
      },
    };
  }

  return (
    <div className="space-y-2 border-b p-2">
      {/* Search moved here from the top bar (TECH-3637): it's part of the
          folder/list menu, and searches notes across every folder. */}
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder="Search notes…"
          className="h-8 w-full pl-8"
        />
      </div>

      {/* Breadcrumb: All notes › Folder › Subfolder, each clickable to jump. */}
      <div className="flex flex-wrap items-center gap-0.5 px-0.5 text-[12px] text-muted-foreground">
        <button
          onClick={() => onNavigate(null)}
          {...dropHandlers("crumb-root", null)}
          className={cn(
            "rounded px-1 py-0.5 hover:bg-muted/50",
            current === null && "font-medium text-foreground",
            dragOver === "crumb-root" && "ring-2 ring-primary ring-inset",
          )}
        >
          All notes
        </button>
        {trail.map((f, i) => (
          <React.Fragment key={f.id}>
            <ChevronRight className="size-3 shrink-0 opacity-60" />
            <button
              onClick={() => onNavigate(f.id)}
              {...dropHandlers(`crumb-${f.id}`, f.id)}
              className={cn(
                "max-w-[10rem] truncate rounded px-1 py-0.5 hover:bg-muted/50",
                i === trail.length - 1 && "font-medium text-foreground",
                dragOver === `crumb-${f.id}` && "ring-2 ring-primary ring-inset",
              )}
            >
              {f.name}
            </button>
          </React.Fragment>
        ))}
      </div>

      {/* Subfolders of the current folder, each a drop target + drill-in row.
          Hidden while searching, since results are a flat cross-folder list. */}
      {!searching && (
        <div className="space-y-0.5">
          {childFolders.map((f) =>
            renamingId === f.id ? (
              <div key={f.id} className="flex items-center gap-1 px-2 py-1">
                <Input
                  autoFocus
                  value={renameName}
                  onChange={(e) => setRenameName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") submitRename();
                    if (e.key === "Escape") {
                      setRenamingId(null);
                      setRenameName("");
                    }
                  }}
                  placeholder="Folder name"
                  className="h-7 text-xs"
                />
                <Button
                  size="sm"
                  className="h-7 shrink-0"
                  onClick={submitRename}
                  disabled={renameFolder.isPending || !renameName.trim()}
                >
                  Save
                </Button>
              </div>
            ) : (
              // A row is a drop target + drill-in button, plus a rename action.
              // The rename pencil is a sibling button (not nested) so the markup
              // stays valid (FIR-1460, request 3).
              <div
                key={f.id}
                {...dropHandlers(f.id, f.id)}
                className={cn(
                  "group flex items-center gap-1 rounded pr-1 hover:bg-muted/50",
                  dragOver === f.id && "ring-2 ring-primary ring-inset",
                )}
              >
                <button
                  onClick={() => onNavigate(f.id)}
                  className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left text-[13px]"
                >
                  <Folder className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="flex-1 truncate">{f.name}</span>
                  {countFor(f.id) > 0 && (
                    <span className="text-[11px] text-muted-foreground">
                      {countFor(f.id)}
                    </span>
                  )}
                </button>
                <button
                  onClick={() => startRename(f)}
                  aria-label={`Rename ${f.name}`}
                  title="Rename folder"
                  className="shrink-0 rounded p-1 text-muted-foreground opacity-0 hover:bg-muted focus:opacity-100 group-hover:opacity-100"
                >
                  <Pencil className="size-3.5" />
                </button>
                {/* FIR-1590: per-folder access control (Only you / Selected
                    colleagues / Whole team). Renders nothing when the flag is
                    off or the folder is open to the whole team for non-owners. */}
                <FolderAccessControl folder={f} />
                <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
              </div>
            ),
          )}
          {adding ? (
            <div className="flex items-center gap-1 px-2 py-1">
              <Input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") submitFolder();
                  if (e.key === "Escape") { setAdding(false); setName(""); }
                }}
                placeholder="Folder name"
                className="h-7 text-xs"
              />
              <Button
                size="sm"
                className="h-7 shrink-0"
                onClick={submitFolder}
                disabled={createFolder.isPending || !name.trim()}
              >
                Add
              </Button>
            </div>
          ) : (
            <button
              onClick={() => setAdding(true)}
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[13px] text-muted-foreground hover:bg-muted/50"
            >
              <FolderPlus className="size-3.5" />
              New folder{current ? " here" : ""}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function NoteListSection({
  label,
  notes,
  selectedId,
  onSelect,
  myId,
  members,
}: {
  label: string;
  notes: Note[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  myId: string | undefined;
  members: OwnerInfo[];
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
          draggable
          onDragStart={(e) => {
            e.dataTransfer.setData(NOTE_DND_TYPE, n.id);
            e.dataTransfer.effectAllowed = "move";
          }}
          className={cn(
            "block w-full cursor-grab border-b px-4 py-2.5 text-left hover:bg-muted/50 active:cursor-grabbing",
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
            {/* Show the owner when it isn't you, so a shared/team note's author
                is always visible (FIR-1460, request 1). */}
            {n.owner_id && n.owner_id !== myId && (
              <>
                <span className="opacity-50">·</span>
                <User className="size-3" />
                <span className="truncate">
                  {ownerName(n.owner_id, myId, members)}
                </span>
              </>
            )}
          </div>
        </button>
      ))}
    </div>
  );
}

export function NoteEditor({
  note,
  wsId,
  onBack,
  onOpenFull,
}: {
  note: Note;
  wsId: string;
  onBack: () => void;
  // TECH-3690: when set, renders an "Åbn fuldt" button in the action bar that
  // jumps to the full Notes surface. Used when the editor is embedded in the
  // inbox detail pane; undefined on the full Notes page (already full).
  onOpenFull?: () => void;
}) {
  const updateNote = useUpdateNote();
  const deleteNote = useDeleteNote();
  const setPin = useSetNotePin();
  const setVisibility = useSetNoteVisibility();

  // Wave 3 (TECH-3556): each surface is independently feature-flagged.
  const lockEnabled = useFeatureFlag("cerebro_note_lock");
  const commentsEnabled = useFeatureFlag("cerebro_note_comments");
  const versionsEnabled = useFeatureFlag("cerebro_note_versions");
  const isMobile = useIsMobile();

  const [sharedIds, setSharedIds] = React.useState<string[]>([]);
  const [showComments, setShowComments] = React.useState(false);
  const [showHistory, setShowHistory] = React.useState(false);
  // FIR-1873: sharing is managed from the "⋯" menu via this sheet, not a
  // top-bar button (Jesper: "begge dele ind i 3 priks menuen").
  const [showShare, setShowShare] = React.useState(false);
  // Add-reference + create-issue are launched from the "⋯" menu (TECH-3690).
  const [addRefOpen, setAddRefOpen] = React.useState(false);
  const [creatingIssue, setCreatingIssue] = React.useState(false);
  const [editor, setEditor] = React.useState<Editor | null>(null);
  const [activeAnchorId, setActiveAnchorId] = React.useState<string | null>(null);
  const [draftQuote, setDraftQuote] = React.useState<string | null>(null);
  const editorRef = React.useRef<ContentEditorRef>(null);
  // Live body driving the outline + word/character count. Seeded from the note
  // and kept current by the editor's debounced onUpdate so the "Oversigt" and
  // counts react while typing, without waiting for the save round-trip.
  const [liveBody, setLiveBody] = React.useState(note.body);
  const contentScrollRef = React.useRef<HTMLDivElement>(null);
  // Inline find & replace, shared with the Documents surface (FIR-1647). The bar
  // operates on the markdown body; replacements remount the editor (replaceToken)
  // so the new text appears and autosaves, mirroring the Documents view.
  const [findOpen, setFindOpen] = React.useState(false);
  const [replaceToken, setReplaceToken] = React.useState(0);

  const myId = useAuthStore((s: { user: { id: string } | null }) => s.user?.id);
  const isOwner = note.owner_id === myId;

  // Edit lock: while this note is open and the flag is on, hold the lock and
  // heartbeat. If someone else holds it live, drop to read-only + offer takeover.
  const editLock = useNoteEditLock(note.id, lockEnabled);
  const readOnly = lockEnabled && editLock.blockedByOther;

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: comments = [] } = useNoteComments(note.id);
  const { data: folders = [] } = useQuery(
    artifactFoldersOptions(wsId, { kind: "note" }),
  );
  const setNoteFolder = useSetNoteFolder();
  const currentFolder = folders.find((f) => f.id === note.folder_id) ?? null;

  // Comment anchors paint an orange highlight over the quoted span (TECH-3637):
  // every root comment/suggestion that has a quote, plus the in-progress draft
  // selection. Only while the comments panel is open, so the editor isn't
  // permanently tinted. The active one (clicked, or the live draft) glows
  // brighter and is scrolled into view.
  const commentAnchors = React.useMemo<CommentAnchor[]>(() => {
    if (!commentsEnabled || !showComments) return [];
    const list: CommentAnchor[] = comments
      .filter((c) => !c.thread_root_id && c.anchor_quote)
      .map((c) => ({ id: c.id, quote: c.anchor_quote as string }));
    if (draftQuote) list.push({ id: DRAFT_ANCHOR_ID, quote: draftQuote });
    return list;
  }, [commentsEnabled, showComments, comments, draftQuote]);

  const activeAnchor = draftQuote ? DRAFT_ANCHOR_ID : activeAnchorId;
  useCommentAnchors(editor, commentAnchors, activeAnchor);

  function startCommentOnSelection(text: string) {
    if (!text) return;
    setDraftQuote(text);
    setActiveAnchorId(null);
    setShowComments(true);
  }

  function closeComments() {
    setShowComments(false);
    setDraftQuote(null);
    setActiveAnchorId(null);
  }

  // The body is the shared rich editor (same as issue comments/descriptions):
  // typing "@" opens the identical mention picker, and inline mentions are
  // stored as markdown that round-trips through the editor. Auto-save on the
  // debounced update and again on blur so nothing is lost on navigate-away.
  function saveBody(markdown: string) {
    setLiveBody(markdown);
    if (markdown !== note.body) updateNote.mutate({ id: note.id, body: markdown });
  }

  // Find & replace hands back the fully replaced body. Remount the editor so the
  // new text shows, keep the outline/counts in sync, and persist it.
  function applyReplacedBody(next: string) {
    setReplaceToken((t) => t + 1);
    saveBody(next);
  }

  // Cmd/Ctrl+F opens the inline find & replace bar (only when editable).
  React.useEffect(() => {
    if (readOnly) return;
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "f") {
        e.preventDefault();
        setFindOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [readOnly]);

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
      {/* Action bar (TECH-3690): one tidy row. Folder + Share sit up front; the
          rest (pin, comments, history, add reference, create issue, delete)
          collapse into a single "⋯" menu so the bar no longer feels cluttered.
          FIR-1676: on mobile the row wraps to a second line instead of clipping
          the "⋯" menu off the right edge (Jesper: "ellers skal der være 2
          rækker"). On desktop it stays a single row with "⋯" pushed right. */}
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5 sm:px-5">
        <Button
          size="sm"
          variant="ghost"
          className="-ml-2 shrink-0 lg:hidden"
          onClick={onBack}
        >
          <ChevronLeft className="size-4" /> Notes
        </Button>

        {/* TECH-3690: embedded in the inbox detail pane → offer a jump to the
            full Notes surface for more room (folder sidebar, history, etc.). */}
        {onOpenFull && (
          <Button
            size="sm"
            variant="outline"
            className="shrink-0 gap-1.5"
            onClick={onOpenFull}
          >
            <ExternalLink className="size-4" /> Åbn fuldt
          </Button>
        )}

        {/* Folder first (request 4 — "folders skal ligge i række 1"). Only the
            owner may move a note; everyone else sees a read-only folder badge
            (FIR-1460, request 2). */}
        {isOwner ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              nativeButton={false}
              render={
                <Badge variant="outline" className="cursor-pointer gap-1.5">
                  <Folder className="size-3" />
                  {currentFolder ? currentFolder.name : "No folder"}
                </Badge>
              }
            />
            <DropdownMenuContent align="start" className="w-56">
              <DropdownMenuRadioGroup
                value={note.folder_id ?? ""}
                onValueChange={(v) =>
                  setNoteFolder.mutate({ id: note.id, folderId: v || null })
                }
              >
                <DropdownMenuLabel>Folder</DropdownMenuLabel>
                <DropdownMenuRadioItem value="">No folder</DropdownMenuRadioItem>
                {folders.map((f) => (
                  <DropdownMenuRadioItem key={f.id} value={f.id}>
                    {f.name}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <Badge variant="outline" className="gap-1.5">
            <Folder className="size-3" />
            {currentFolder ? currentFolder.name : "No folder"}
          </Badge>
        )}

        {/* FIR-1873: the owner now manages sharing from the "⋯" menu (it opens
            the Share sheet), and the owner badge is gone from this bar — the
            owner is already shown under the title (EntityMetaHeader). A
            non-owner can't change sharing, so they keep a read-only badge here
            showing who can currently see the note. */}
        {!isOwner && (
          <Badge variant="outline" className="gap-1.5">
            {VIS_ICON[note.visibility]}
            <span>{VISIBILITY_LABELS[note.visibility]}</span>
          </Badge>
        )}

        {/* Everything else lives behind one shared "⋯" menu — the same
            component the Documents view uses (FIR-1647, request 5 + 6). */}
        <EditorActionsMenu
          triggerLabel="Note actions"
          className="ml-auto"
          items={[
            // Share lives in the menu now (FIR-1873). Owner-only — only the
            // owner may change who can see the note; it opens the Share sheet.
            isOwner && {
              key: "share",
              label: "Share",
              icon: Share2,
              onSelect: () => setShowShare(true),
            },
            // Pin is an owner-only action on the backend (FIR-1460, request 2).
            isOwner && {
              key: "pin",
              label: note.pinned ? "Unpin" : "Pin",
              icon: Pin,
              onSelect: () =>
                setPin.mutate({ id: note.id, pinned: !note.pinned }),
            },
            commentsEnabled && {
              key: "comments",
              label: "Comments",
              icon: MessageSquare,
              onSelect: () =>
                showComments ? closeComments() : setShowComments(true),
            },
            versionsEnabled && {
              key: "history",
              label: "History",
              icon: History,
              onSelect: () => setShowHistory(true),
            },
            !readOnly && {
              key: "find-replace",
              label: "Find & replace",
              icon: Replace,
              onSelect: () => setFindOpen(true),
            },
            {
              key: "add-reference",
              label: "Add reference",
              icon: Link2,
              onSelect: () => setAddRefOpen(true),
            },
            {
              key: "create-issue",
              label: "Create issue",
              icon: ListPlus,
              onSelect: () => setCreatingIssue(true),
            },
            // Delete is owner-only (the backend rejects others with 403). Show
            // it solely to the owner so the action never silently fails
            // (FIR-1460, request 2). The onSuccess navigates back to the
            // overview once the note is gone — without it selectedId still
            // points at the deleted note, which on mobile leaves a blank
            // screen instead of the note list (TECH-3770).
            isOwner && {
              key: "delete",
              label: "Delete",
              icon: Trash2,
              destructive: true,
              separatorBefore: true,
              onSelect: () =>
                deleteNote.mutate(note.id, { onSuccess: () => onBack() }),
            },
          ]}
        />
      </div>

      <div className="flex min-h-0 flex-1">
        <div
          ref={contentScrollRef}
          className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-6"
        >
          <EditableTitle
            value={note.title}
            onSave={(next) => updateNote.mutate({ id: note.id, title: next })}
            readOnly={readOnly}
            placeholder="Title (optional — first line becomes the title)"
          />

          {/* Shared metadata header (FIR-1647, request 8): the same owner +
              "Updated <date>" strip the Documents view shows. A note's owner_id
              is a member id, so it maps onto the member author slot. FIR-1852:
              a note now carries the same issue/project scope a document does, so
              when set it renders "on FIR-XXX" here, identical to the Documents
              view. */}
          <EntityMetaHeader
            authorType="member"
            authorId={note.owner_id}
            updatedAt={note.updated_at}
            issueId={note.issue_id}
            projectId={note.project_id}
          />

          <NoteReferences noteId={note.id} />

          {/* Body uses the SAME rich editor as issue comments + descriptions, so
              "@" behaves identically (people, agents, issues, …) inline. When
              the note is locked by someone else we render it read-only instead.
              The bubble menu's "Comment" icon (commentsEnabled) opens the
              comments panel anchored to the selected text. */}
          {readOnly ? (
            <ReadonlyContent content={note.body} className="min-h-[50vh] flex-1" />
          ) : (
            <div className="flex min-h-0 flex-1 flex-col">
              {findOpen && (
                <FindReplaceBar
                  body={liveBody}
                  onReplaceAll={applyReplacedBody}
                  onReplaceFirst={applyReplacedBody}
                  onClose={() => setFindOpen(false)}
                />
              )}
              <ContentEditor
                key={`${note.id}:${replaceToken}`}
                ref={editorRef}
                defaultValue={liveBody}
                onUpdate={saveBody}
                onBlur={() => saveBody(editorRef.current?.getMarkdown() ?? "")}
                onEditorReady={setEditor}
                onCommentOnSelection={
                  commentsEnabled ? startCommentOnSelection : undefined
                }
                placeholder="Just start writing… (type “@” to mention a person, agent or issue)"
                className="min-h-[50vh] flex-1"
              />
            </div>
          )}
        </div>

        {/* Heading navigation ("Oversigt") + word/character count, shared with
            the Documents view (TECH-3637). Hidden below lg and when there are
            <2 headings. */}
        <div className="hidden shrink-0 overflow-auto py-6 pr-4 lg:block">
          <DocumentToolsSidebar body={liveBody} contentRef={contentScrollRef} />
        </div>

        {/* Desktop: comments as an inline side rail. */}
        {commentsEnabled && showComments && !isMobile && (
          <div className="w-80 shrink-0 border-l">
            <NoteCommentsPanel
              noteId={note.id}
              noteBody={note.body}
              isOwner={isOwner}
              draftQuote={draftQuote}
              activeAnchorId={activeAnchorId}
              onClearDraft={() => setDraftQuote(null)}
              onSelectThread={(id) => {
                setDraftQuote(null);
                setActiveAnchorId(id);
              }}
              onClose={closeComments}
            />
          </div>
        )}
      </div>

      {/* Mobile: there's no room for a side rail, so comments open as a
          near-full-width sheet. Without this, tapping "Comment" on mobile did
          nothing (TECH-3637). */}
      {commentsEnabled && isMobile && (
        <Sheet
          open={showComments}
          onOpenChange={(o) => {
            if (!o) closeComments();
          }}
        >
          <SheetContent
            side="right"
            showCloseButton={false}
            className="flex flex-col p-0 data-[side=right]:w-[94vw]"
          >
            <SheetHeader className="sr-only">
              <SheetTitle>Comments</SheetTitle>
            </SheetHeader>
            <NoteCommentsPanel
              noteId={note.id}
              noteBody={note.body}
              isOwner={isOwner}
              draftQuote={draftQuote}
              activeAnchorId={activeAnchorId}
              onClearDraft={() => setDraftQuote(null)}
              onSelectThread={(id) => {
                setDraftQuote(null);
                setActiveAnchorId(id);
              }}
              onClose={closeComments}
            />
          </SheetContent>
        </Sheet>
      )}

      {/* FIR-1873: Share moved out of the top bar into this sheet, opened from
          the "⋯" menu. Same controls as before — visibility + share-with — so
          behavior is unchanged, only the entry point moved. Owner-only. */}
      {isOwner && (
        <Sheet open={showShare} onOpenChange={setShowShare}>
          <SheetContent
            side="right"
            className="flex flex-col gap-0 p-0 data-[side=right]:w-[94vw] data-[side=right]:sm:max-w-md"
          >
            <SheetHeader className="border-b px-5 py-3">
              <SheetTitle className="flex items-center gap-2 text-base">
                <Share2 className="size-4" /> Share
              </SheetTitle>
              <SheetDescription className="sr-only">
                Choose who can see this note.
              </SheetDescription>
            </SheetHeader>
            <div className="flex flex-col gap-1 overflow-y-auto p-3">
              <div className="px-1 pb-1 text-xs font-medium text-muted-foreground">
                Who can see it
              </div>
              {(
                [
                  ["private", "Only you"],
                  ["shared", "Selected colleagues"],
                  ["workspace", "Whole team"],
                ] as [NoteVisibility, string][]
              ).map(([v, label]) => (
                <button
                  key={v}
                  onClick={() => applyVisibility(v)}
                  className="flex items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-muted/50"
                >
                  {VIS_ICON[v]}
                  <span className="flex-1">{label}</span>
                  {note.visibility === v && <Check className="size-4" />}
                </button>
              ))}
              {note.visibility === "shared" && members.length > 0 && (
                <>
                  <div className="mt-2 px-1 pb-1 text-xs font-medium text-muted-foreground">
                    Share with
                  </div>
                  {members.map((m) => (
                    <button
                      key={m.user_id}
                      onClick={() => toggleShare(m.user_id)}
                      className="flex items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-muted/50"
                    >
                      <span className="flex-1 truncate">{m.name}</span>
                      {sharedIds.includes(m.user_id) && (
                        <Check className="size-4" />
                      )}
                    </button>
                  ))}
                </>
              )}
            </div>
          </SheetContent>
        </Sheet>
      )}

      {versionsEnabled && (
        <NoteVersionsDialog
          noteId={note.id}
          open={showHistory}
          onOpenChange={setShowHistory}
        />
      )}

      <NoteAddReferenceDialog
        noteId={note.id}
        open={addRefOpen}
        onOpenChange={setAddRefOpen}
      />
      <NoteCreateIssueDialog
        note={note}
        open={creatingIssue}
        onOpenChange={setCreatingIssue}
      />
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
