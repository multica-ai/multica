"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Plus,
  Search,
  Pin,
  Trash2,
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
  Check,
  Copy,
  GitMerge,
  Users,
  PenLine,
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
  FolderSuggestionBanner,
} from "@multica/cerebro-artifacts/views/components";
import { FolderAccessColumn } from "@multica/cerebro-collections/views";
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
  type ContentEditorRef,
  ReadonlyContent,
} from "@multica/views/editor";
import { EditorImageTray } from "@multica/cerebro-composer";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useEnsureNoteMentionScope } from "./note-mention-scope";
import type { Editor } from "@tiptap/react";
import {
  notesListOptions,
  noteDetailOptions,
  useCreateNote,
  useUpdateNote,
  useDeleteNote,
  useSetNotePin,
  useNoteEditLock,
  useNoteComments,
  useSetNoteFolder,
  useSetNoteAuthorCodes,
  firstLineTitle,
  memberCode,
  NoteConflictError,
} from "../core";
import type { Note } from "../core";
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
import { NoteConflictDialog } from "./note-conflict-dialog";
import type { NoteConflict } from "./note-conflict-dialog";
import { useCommentAnchors } from "./use-comment-anchors";
import { useFindHighlight } from "./use-find-highlight";
import { useAuthorCodeStamper } from "./use-author-codes";
import { LineAuthorsGutter } from "./line-authors-gutter";
import { useLineAuthorsPull } from "./use-line-authors-pull";
import {
  DRAFT_ANCHOR_ID,
  type CommentAnchor,
} from "./comment-anchor-plugin";

// drag-and-drop payload type for moving a note row onto a folder.
const NOTE_DND_TYPE = "application/x-note-id";

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
export function NotesPage({
  initialNoteId,
  initialCommentId,
}: {
  initialNoteId?: string | null;
  // FIR-2589: when the Notes surface is opened from a note-comment mention in the
  // inbox, this is the comment id to open — the editor opens the comments panel
  // and scrolls to that comment.
  initialCommentId?: string | null;
}) {
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
  const navigation = useNavigation();
  const wsPaths = useWorkspacePaths();
  // FIR-2688: seed the open folder from `?folder=<id>` so a folder URL copied
  // from the address bar reopens that folder. A stale id resolves to an empty
  // list until refetch, which is harmless.
  const [folderId, setFolderId] = React.useState<string | null>(
    () => navigation.searchParams.get("folder"),
  );
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
  const urlNoteId = navigation.searchParams.get("note");
  const urlCommentId = navigation.searchParams.get("comment");
  const initialCommentForSelected =
    selectedId && (selectedId === initialNoteId || selectedId === urlNoteId)
      ? initialCommentId ?? urlCommentId
      : null;

  React.useEffect(() => {
    const noteFromUrl = initialNoteId ?? urlNoteId;
    if (!selectedId && noteFromUrl) setSelectedId(noteFromUrl);
  }, [initialNoteId, selectedId, urlNoteId]);

  // FIR-2595 + FIR-2688: keep the browser address bar in sync with the open
  // note OR the open folder, so both URLs are shareable ("kopier den fra
  // browseren"). `replaceSilent` updates the URL via the History API with no
  // route reload — the same trick the inbox uses (FIR-2684). Deliberately NOT
  // falling back to `replace`: web-only, it is gated on `replaceSilent` being
  // present. On desktop there is no URL bar and a real `replace` would swap the
  // route element and remount the page, so desktop shares via the Copy link
  // button. An open note wins the URL (`/notes/:id`); with no note open the URL
  // reflects the folder (`/notes?folder=<id>`), else the bare list. Comparing
  // the full pathname+search keeps it loop-free.
  React.useEffect(() => {
    if (!navigation.replaceSilent) return;
    if (!navigation.pathname.includes("/notes")) return;
    if (selectedId) {
      // Note open: `/notes/:id`. Guard on pathname so a deep link like
      // /notes/:id?comment=x keeps its ?comment param untouched on load.
      // FIR-2826 — when the legacy `/notes?note=<id>&comment=<comment-id>`
      // entry route is normalized to `/notes/<id>`, keep the comment param so
      // the route remount still passes initialCommentId to NoteEditor.
      const commentParam = initialCommentForSelected
        ? `?comment=${encodeURIComponent(initialCommentForSelected)}`
        : "";
      const desired = `${wsPaths.noteDetail(selectedId)}${commentParam}`;
      const searchString = navigation.searchParams.toString();
      const current = searchString
        ? `${navigation.pathname}?${searchString}`
        : navigation.pathname;
      if (current === desired) return;
      navigation.replaceSilent(desired);
      return;
    }
    if (urlNoteId) return;
    // No note open: the folder owns the URL. Write unconditionally so leaving a
    // folder clears a stale `?folder`; History-API writes don't re-render, so
    // the effect only fires on a real selection change (loop-free).
    navigation.replaceSilent(
      folderId ? wsPaths.notesFolder(folderId) : wsPaths.notes(),
    );
  }, [selectedId, folderId, wsPaths, navigation, urlNoteId, initialCommentForSelected]);

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
              initialCommentId={initialCommentForSelected}
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
                {/* FIR-1590 → Collections: per-folder grant editor. */}
                <FolderAccessColumn
                  surface="artifact"
                  folderId={f.id}
                  folderName={f.name}
                />
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
            {/* FIR-2595: per-note visibility ("Only you" / share) is removed —
                folders drive who can open and edit a note. Show the owner when
                it isn't you (FIR-1460, request 1). */}
            {n.owner_id && n.owner_id !== myId && (
              <>
                <User className="size-3" />
                <span className="truncate">
                  {ownerName(n.owner_id, myId, members)}
                </span>
              </>
            )}
            {/* FIR-2145: comment count indicator — only shown when > 0. */}
            {n.comment_count > 0 && (
              <>
                {n.owner_id && n.owner_id !== myId && (
                  <span className="opacity-50">·</span>
                )}
                <MessageSquare className="size-3" />
                <span>{n.comment_count}</span>
              </>
            )}
          </div>
        </button>
      ))}
    </div>
  );
}

// FIR-2826 — the desktop comments rail is wider by default and drag-resizable.
// Width is clamped and remembered per browser so it survives reloads.
const COMMENTS_WIDTH_KEY = "note:comments-width";
const COMMENTS_MIN_WIDTH = 300;
const COMMENTS_MAX_WIDTH = 760;
const COMMENTS_DEFAULT_WIDTH = 420;

export function NoteEditor({
  note,
  wsId,
  onBack,
  onOpenFull,
  initialCommentId,
}: {
  note: Note;
  wsId: string;
  onBack: () => void;
  // TECH-3690: when set, renders an "Open full" button in the action bar that
  // jumps to the full Notes surface. Used when the editor is embedded in the
  // inbox detail pane; undefined on the full Notes page (already full).
  onOpenFull?: () => void;
  // FIR-2589: a comment id to open on mount (from a note-comment mention in the
  // inbox). Opens the comments panel and scrolls to that comment.
  initialCommentId?: string | null;
}) {
  const updateNote = useUpdateNote();
  const deleteNote = useDeleteNote();
  const setPin = useSetNotePin();

  // Wave 3 (TECH-3556): each surface is independently feature-flagged.
  const lockEnabled = useFeatureFlag("cerebro_note_lock");
  const commentsEnabled = useFeatureFlag("cerebro_note_comments");
  const versionsEnabled = useFeatureFlag("cerebro_note_versions");
  // FIR-2595 point 3: scope the note's @mention picker to people with access.
  const scopedMentions = useFeatureFlag("cerebro_note_scoped_mentions");
  useEnsureNoteMentionScope(wsId, note.id, scopedMentions);
  // FIR-1317 Plan A: per-note toggle for conflict merge (moved out of the
  // workspace feature flag so each user can turn it on/off from the note ⋯ menu).
  // Defaults to ON; stored in localStorage so the preference survives a reload.
  const [conflictMergeEnabled, setConflictMergeEnabled] = React.useState(
    () => localStorage.getItem(`note:conflict-merge:${note.id}`) !== "0",
  );
  function toggleConflictMerge() {
    const next = !conflictMergeEnabled;
    setConflictMergeEnabled(next);
    localStorage.setItem(`note:conflict-merge:${note.id}`, next ? "1" : "0");
  }
  // FIR-2810: per-note (and per-user) toggle showing the line-authors gutter —
  // who wrote / last edited every line, Apple Notes-style. Defaults to OFF;
  // stored in localStorage like the conflict-merge toggle above.
  const lineAuthorsEnabled = useFeatureFlag("cerebro_note_line_authors");
  const [showLineAuthors, setShowLineAuthors] = React.useState(
    () => localStorage.getItem(`note:line-authors:${note.id}`) === "1",
  );
  function toggleLineAuthors() {
    const next = !showLineAuthors;
    setShowLineAuthors(next);
    localStorage.setItem(`note:line-authors:${note.id}`, next ? "1" : "0");
  }
  const setAuthorCodesMutation = useSetNoteAuthorCodes();
  const isMobile = useIsMobile();

  // FIR-1317 Plan A: track the server's updated_at when we last fetched/saved
  // the note. Sent with every save so the backend can detect concurrent edits.
  // Seeded from the note's updated_at on mount; refreshed after each successful save.
  const baseUpdatedAt = React.useRef<string>(note.updated_at);

  // FIR-1317 Plan A: active conflict waiting for user resolution.
  const [conflict, setConflict] = React.useState<NoteConflict | null>(null);

  const [showComments, setShowComments] = React.useState(false);
  const [showHistory, setShowHistory] = React.useState(false);
  // FIR-2826 — remembered width of the desktop comments rail (drag-resizable via
  // the handle on its left edge). Seeded from localStorage, clamped to sane
  // bounds so a stale/garbage value can't collapse or overflow the rail.
  const [commentsWidth, setCommentsWidth] = React.useState<number>(() => {
    const saved = Number(localStorage.getItem(COMMENTS_WIDTH_KEY));
    return Number.isFinite(saved) &&
      saved >= COMMENTS_MIN_WIDTH &&
      saved <= COMMENTS_MAX_WIDTH
      ? saved
      : COMMENTS_DEFAULT_WIDTH;
  });
  const startCommentsResize = React.useCallback(
    (e: React.PointerEvent) => {
      e.preventDefault();
      const startX = e.clientX;
      const startW = commentsWidth;
      let latest = startW;
      const onMove = (ev: PointerEvent) => {
        // The rail sits on the right edge, so dragging its left handle to the
        // left (smaller clientX) widens it.
        const next = Math.min(
          COMMENTS_MAX_WIDTH,
          Math.max(COMMENTS_MIN_WIDTH, startW + (startX - ev.clientX)),
        );
        latest = next;
        setCommentsWidth(next);
      };
      const onUp = () => {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        document.body.style.userSelect = "";
        localStorage.setItem(COMMENTS_WIDTH_KEY, String(Math.round(latest)));
      };
      // Suppress text selection while dragging across the editor.
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    },
    [commentsWidth],
  );
  // Add-reference + create-issue are launched from the "⋯" menu (TECH-3690).
  const [addRefOpen, setAddRefOpen] = React.useState(false);
  const [creatingIssue, setCreatingIssue] = React.useState(false);
  const [editor, setEditor] = React.useState<Editor | null>(null);
  const [activeAnchorId, setActiveAnchorId] = React.useState<string | null>(null);
  const [draftQuote, setDraftQuote] = React.useState<string | null>(null);
  const editorRef = React.useRef<ContentEditorRef>(null);
  // Uploader for images dropped/pasted into the note (image tray, FIR-2693).
  const { uploadWithToast } = useFileUpload(api);
  // Live body driving the outline + word/character count. Seeded from the note
  // and kept current by the editor's debounced onUpdate so the "Oversigt" and
  // counts react while typing, without waiting for the save round-trip.
  const [liveBody, setLiveBody] = React.useState(note.body);
  const contentScrollRef = React.useRef<HTMLDivElement>(null);
  // FIR-2145: closing the comments panel reflows the flex layout (the 320px
  // sidebar disappears), which causes browsers to reset scrollTop on the content
  // div. Save it right before the state flip and restore it in useLayoutEffect,
  // which fires after the DOM mutation but before the browser paints.
  const savedScrollRef = React.useRef<number>(0);
  // Inline find & replace, shared with the Documents surface (FIR-1647). The bar
  // operates on the markdown body; replacements remount the editor (replaceToken)
  // so the new text appears and autosaves, mirroring the Documents view.
  const [findOpen, setFindOpen] = React.useState(false);
  const [replaceToken, setReplaceToken] = React.useState(0);
  // FIR-2145: lifted state drives the find-highlight decoration plugin so the
  // editor paints matches while the user types in the FindReplaceBar.
  const [findQuery, setFindQuery] = React.useState("");
  const [findActiveIndex, setFindActiveIndex] = React.useState(-1);

  const myId = useAuthStore((s: { user: { id: string } | null }) => s.user?.id);
  const isOwner = note.owner_id === myId;

  // FIR-2595: "Copy link" — puts a shareable URL to this note on the clipboard.
  // getShareableUrl returns the web origin + path (desktop: the connected
  // environment's public web URL), so the link opens the note for anyone with
  // access. Available to everyone, not just the owner.
  const navigation = useNavigation();
  const wsPaths = useWorkspacePaths();
  const [linkCopied, setLinkCopied] = React.useState(false);
  const copyNoteLink = React.useCallback(() => {
    const url = navigation.getShareableUrl(wsPaths.noteDetail(note.id));
    void navigator.clipboard.writeText(url);
    setLinkCopied(true);
    window.setTimeout(() => setLinkCopied(false), 1500);
  }, [navigation, wsPaths, note.id]);

  // Edit lock: while this note is open and the flag is on, hold the lock and
  // heartbeat. If someone else holds it live, drop to read-only + offer takeover.
  const editLock = useNoteEditLock(note.id, lockEnabled);
  // FIR-2595: authoritative edit permission comes from the SINGLE-note read.
  // The editor is opened from a list row, and list rows omit can_edit (the
  // schema defaults it to true), so trusting note.can_edit here made the editor
  // writable for everyone — a non-owner could type into a save that then 403s
  // silently. Fetch the note detail and gate on ITS can_edit. The owner is
  // always allowed; a non-owner stays read-only until the fetch confirms edit
  // access. It is also read-only while another user holds the live edit lock.
  const { data: noteDetail } = useQuery(noteDetailOptions(wsId, note.id));
  const canEdit = isOwner || noteDetail?.can_edit === true;
  const readOnly = !canEdit || (lockEnabled && editLock.blockedByOther);

  // FIR-2810: line attribution + author codes. The single-note read carries
  // the per-line attribution and the note's author-codes toggle; member names
  // resolve the stored user ids into member codes ("JEH") for the gutter and
  // the stamper.
  const authorCodesOn = noteDetail?.author_codes === true;
  const lineAttrs = React.useMemo(
    () => noteDetail?.line_attrs ?? [],
    [noteDetail],
  );
  const { data: gutterMembers = [] } = useQuery(memberListOptions(wsId));
  const membersById = React.useMemo(
    () =>
      new Map(
        (gutterMembers as OwnerInfo[]).map((m) => [
          m.user_id,
          { name: m.name },
        ]),
      ),
    [gutterMembers],
  );
  const myCode = memberCode(
    (myId && membersById.get(myId)?.name) || "",
  );
  useAuthorCodeStamper(
    editor,
    lineAuthorsEnabled && authorCodesOn && !readOnly,
    myCode,
  );
  const showGutter = lineAuthorsEnabled && showLineAuthors;
  const editorWrapRef = React.useRef<HTMLDivElement>(null);
  // FIR-2810: temporary reveal — drag the left-edge handle (desktop) or touch
  // and pull right on the body (mobile). Released past the threshold it stays
  // open; a small pull back to the left closes it. Disabled while the
  // permanent toggle already shows it.
  const contentSlideRef = React.useRef<HTMLDivElement>(null);
  const gutterBoxRef = React.useRef<HTMLDivElement>(null);
  const pull = useLineAuthorsPull({
    enabled: lineAuthorsEnabled && !showGutter,
    slideRef: contentSlideRef,
    gutterRef: gutterBoxRef,
  });
  const gutterMounted = showGutter || pull.visible;
  // Bump after every (debounced) editor change so the gutter re-measures the
  // rendered blocks; also when the comments rail opens/closes (layout reflow).
  const [gutterVersion, setGutterVersion] = React.useState(0);
  React.useEffect(() => {
    if (gutterMounted) setGutterVersion((v) => v + 1);
  }, [gutterMounted, liveBody, showComments, readOnly]);

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

  // FIR-2589: opened from a note-comment mention in the inbox — open the
  // comments panel and highlight the mentioned comment's thread root (a reply
  // resolves to its root, which is what the panel highlights). Runs once, after
  // the comments load, so we can resolve a reply to its root.
  const openedCommentRef = React.useRef(false);
  React.useEffect(() => {
    if (openedCommentRef.current) return;
    if (!commentsEnabled || !initialCommentId || comments.length === 0) return;
    const target = comments.find((c) => c.id === initialCommentId);
    if (!target) return;
    openedCommentRef.current = true;
    setActiveAnchorId(target.thread_root_id ?? target.id);
    setShowComments(true);
  }, [commentsEnabled, initialCommentId, comments]);
  // FIR-2145: paint find matches in the editor as the user types.
  useFindHighlight(editor, findOpen ? findQuery : "", findOpen ? findActiveIndex : -1);

  function startCommentOnSelection(text: string) {
    if (!text) return;
    setDraftQuote(text);
    setActiveAnchorId(null);
    setShowComments(true);
  }

  function closeComments() {
    if (contentScrollRef.current) {
      savedScrollRef.current = contentScrollRef.current.scrollTop;
    }
    setShowComments(false);
    setDraftQuote(null);
    setActiveAnchorId(null);
  }

  // FIR-2145: restore the scroll position saved in closeComments() after the
  // layout reflow caused by the sidebar disappearing.
  React.useLayoutEffect(() => {
    if (!showComments && savedScrollRef.current > 0 && contentScrollRef.current) {
      contentScrollRef.current.scrollTop = savedScrollRef.current;
    }
  }, [showComments]);

  // The body is the shared rich editor (same as issue comments/descriptions):
  // typing "@" opens the identical mention picker, and inline mentions are
  // stored as markdown that round-trips through the editor. Auto-save on the
  // debounced update and again on blur so nothing is lost on navigate-away.
  // FIR-1317 Plan A: sends base_updated_at (when conflict merge is on) so the
  // backend can detect if someone else saved in the meantime. On a 409 the
  // NoteConflictError is caught and the merge dialog opens.
  //
  // FIR-2810 follow-up: saves are SERIALIZED — at most one in flight, the
  // newest body queued behind it. Firing a second save before the first
  // response refreshed baseUpdatedAt made the backend see a stale base and
  // answer 409, so the "two people edited this note" dialog opened for a user
  // typing alone (the author-code stamp plus the blur + debounce double-save
  // made this frequent). lastSentBody additionally drops no-change saves.
  const saveInFlight = React.useRef(false);
  const queuedBody = React.useRef<string | null>(null);
  const lastSentBody = React.useRef<string | null>(note.body);

  function saveBody(markdown: string) {
    setLiveBody(markdown);
    pushSave(markdown);
  }

  function pushSave(markdown: string) {
    if (saveInFlight.current) {
      queuedBody.current = markdown;
      return;
    }
    if (markdown === lastSentBody.current) return;
    saveInFlight.current = true;
    lastSentBody.current = markdown;
    updateNote.mutate(
      {
        id: note.id,
        body: markdown,
        baseUpdatedAt: conflictMergeEnabled ? baseUpdatedAt.current : undefined,
      },
      {
        onSuccess: (saved) => {
          // Refresh the base so subsequent saves don't false-positive.
          if (saved?.updated_at) baseUpdatedAt.current = saved.updated_at;
        },
        onError: (err) => {
          if (err instanceof NoteConflictError) {
            // The dialog takes over; anything queued predates the conflict
            // and must not fire behind the user's resolution.
            queuedBody.current = null;
            setConflict(err.conflict);
          } else {
            // Failed save: forget the body so the next edit retries it.
            lastSentBody.current = null;
          }
        },
        onSettled: () => {
          saveInFlight.current = false;
          const next = queuedBody.current;
          queuedBody.current = null;
          if (next !== null) pushSave(next);
        },
      },
    );
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
          the "⋯" menu off the right edge (Jesper: otherwise there must be two
          rows). On desktop it stays a single row with "⋯" pushed right. */}
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
            <ExternalLink className="size-4" /> Open full
          </Button>
        )}

        {/* Folder first (request 4 — "folders must be on row 1"). Only the
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


        {/* Comments button: surfaces directly in the action bar so the count
            badge is always visible when comments exist and the panel is closed.
            FIR-2145: Jesper: "a small marking" when comments are present. */}
        {commentsEnabled && (
          <Button
            size="sm"
            variant={showComments ? "secondary" : "ghost"}
            onClick={() => (showComments ? closeComments() : setShowComments(true))}
            aria-label={showComments ? "Close comments" : "Comments"}
            className="relative shrink-0"
          >
            <MessageSquare className="size-4" />
            {!showComments && comments.length > 0 && (
              <span className="absolute -right-1 -top-1 flex h-4 min-w-[1rem] items-center justify-center rounded-full bg-primary px-1 text-[10px] font-medium text-primary-foreground">
                {comments.length}
              </span>
            )}
          </Button>
        )}

        {/* Everything else lives behind one shared "⋯" menu — the same
            component the Documents view uses (FIR-1647, request 5 + 6). */}
        <EditorActionsMenu
          triggerLabel="Note actions"
          className="ml-auto"
          items={[
            // FIR-2595: Copy link — a shareable URL to this note. Available to
            // everyone, so anyone viewing a note can hand the link on.
            {
              key: "copy-link",
              label: linkCopied ? "Link copied" : "Copy link",
              icon: linkCopied ? Check : Copy,
              onSelect: copyNoteLink,
            },
            // FIR-2595: per-note "Share" (visibility + share-with) is removed —
            // access is managed on the folder via Collections, not per note.
            // Pin is an owner-only action on the backend (FIR-1460, request 2).
            isOwner && {
              key: "pin",
              label: note.pinned ? "Unpin" : "Pin",
              icon: Pin,
              onSelect: () =>
                setPin.mutate({ id: note.id, pinned: !note.pinned }),
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
            // FIR-1317: per-note conflict-merge toggle. Visible directly in the
            // note so any user can switch it on/off without touching workspace settings.
            {
              key: "conflict-merge",
              label: conflictMergeEnabled
                ? "Conflict merge: On"
                : "Conflict merge: Off",
              icon: GitMerge,
              onSelect: toggleConflictMerge,
            },
            // FIR-2810: show/hide the line-authors gutter (who wrote / last
            // edited every line). Per-user view preference, like conflict merge.
            lineAuthorsEnabled && {
              key: "line-authors",
              label: showLineAuthors
                ? "Line authors: On"
                : "Line authors: Off",
              icon: Users,
              onSelect: toggleLineAuthors,
            },
            // FIR-2810: per-note author-codes toggle — stamp the writer's
            // member code (e.g. JEH) on every line they write. Saved on the
            // note itself, so it is on for everyone who writes in it.
            lineAuthorsEnabled &&
              !readOnly && {
                key: "author-codes",
                label: authorCodesOn
                  ? "Author codes: On"
                  : "Author codes: Off",
                icon: PenLine,
                onSelect: () =>
                  setAuthorCodesMutation.mutate({
                    id: note.id,
                    authorCodes: !authorCodesOn,
                  }),
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

          {/* FIR-2697 part 2 — a pending agent folder suggestion for this note.
              A note is an artifact, so it reuses the Documents banner. */}
          <FolderSuggestionBanner artifactId={note.id} canResolve={canEdit} />

          {/* Body uses the SAME rich editor as issue comments + descriptions, so
              "@" behaves identically (people, agents, issues, …) inline. When
              the note is locked by someone else we render it read-only instead.
              The bubble menu's "Comment" icon (commentsEnabled) opens the
              comments panel anchored to the selected text. */}
          {/* FIR-2810: the relative wrapper hosts the line-authors gutter,
              which measures the rendered blocks inside it. With the gutter on,
              the body shifts right to make room for the attribution column.
              With it off, a click-and-drag on the left-edge handle (desktop)
              or a touch-and-pull right on the body (mobile) slides the body
              aside; past the threshold it latches open until pulled back. */}
          <div
            ref={editorWrapRef}
            {...pull.wrapperProps}
            className={cn(
              "relative flex min-h-0 flex-1 flex-col overflow-x-clip",
              showGutter && "pl-24",
            )}
          >
          {gutterMounted && (
            <div
              ref={gutterBoxRef}
              className="absolute inset-y-0 left-0 w-24"
              style={showGutter ? undefined : { opacity: 0 }}
            >
              <LineAuthorsGutter
                contentRef={editorWrapRef}
                body={noteDetail?.body ?? note.body}
                attrs={lineAttrs}
                membersById={membersById}
                version={gutterVersion}
              />
            </div>
          )}
          {lineAuthorsEnabled && !showGutter && !isMobile && (
            // Desktop grab handle for the temporary peek; the faint bar shows
            // on hover so the affordance is discoverable without being loud.
            <div
              {...pull.stripProps}
              aria-hidden
              className="group absolute inset-y-0 left-0 z-10 w-3 cursor-ew-resize touch-none"
            >
              <div className="absolute left-[3px] top-1/2 h-10 w-1 -translate-y-1/2 rounded-full bg-border opacity-0 transition-opacity group-hover:opacity-100" />
            </div>
          )}
          <div
            ref={contentSlideRef}
            className="flex min-h-0 flex-1 flex-col bg-background"
          >
          {readOnly ? (
            <ReadonlyContent content={note.body} className="min-h-[50vh] flex-1" />
          ) : (
            <div className="flex min-h-0 flex-1 flex-col">
              {findOpen && (
                <FindReplaceBar
                  body={liveBody}
                  onReplaceAll={applyReplacedBody}
                  onReplaceFirst={applyReplacedBody}
                  onClose={() => {
                    setFindOpen(false);
                    setFindQuery("");
                    setFindActiveIndex(-1);
                  }}
                  onFindStateChange={(q, idx) => {
                    setFindQuery(q);
                    setFindActiveIndex(idx);
                  }}
                />
              )}
              <EditorImageTray
                key={`${note.id}:${replaceToken}`}
                ref={editorRef}
                defaultValue={liveBody}
                onUpdate={saveBody}
                onUploadFile={uploadWithToast}
                onBlur={() => saveBody(editorRef.current?.getMarkdown() ?? "")}
                onEditorReady={setEditor}
                onCommentOnSelection={
                  commentsEnabled ? startCommentOnSelection : undefined
                }
                // FIR-2595 point 3: scope @mentions to people with note access.
                currentNoteId={scopedMentions ? note.id : undefined}
                placeholder="Just start writing… (type “@” to mention a person, agent or issue)"
                className="min-h-[50vh] flex-1"
              />
            </div>
          )}
          </div>
          </div>
        </div>

        {/* Heading navigation ("Oversigt") + word/character count, shared with
            the Documents view (TECH-3637). Hidden below lg and when there are
            <2 headings. */}
        <div className="hidden shrink-0 overflow-auto py-6 pr-4 lg:block">
          <DocumentToolsSidebar body={liveBody} contentRef={contentScrollRef} />
        </div>

        {/* Desktop: comments as an inline side rail. FIR-2826 — wider default
            and drag-resizable via the handle on its left edge. */}
        {commentsEnabled && showComments && !isMobile && (
          <div
            className="relative shrink-0 border-l"
            style={{ width: commentsWidth }}
          >
            {/* Drag handle: a hit strip straddling the left border with a
                thin bar that brightens on hover/drag. */}
            <div
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize comments panel"
              onPointerDown={startCommentsResize}
              className="group absolute -left-1 top-0 bottom-0 z-10 w-2 cursor-col-resize"
            >
              <div className="mx-auto h-full w-px bg-transparent transition-colors group-hover:bg-primary/40 group-active:bg-primary/60" />
            </div>
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

      {/* FIR-1317 Plan A: conflict merge dialog. Shown when two people save
          the same note at the same time and the backend returns 409. */}
      {conflictMergeEnabled && (
        <NoteConflictDialog
          conflict={conflict}
          onResolve={(resolvedBody) => {
            setConflict(null);
            // Save the resolved body without the base_updated_at check so it
            // goes through unconditionally (the user just made the decision).
            // Runs through the same in-flight bookkeeping as pushSave so a
            // queued autosave can never race the resolution.
            saveInFlight.current = true;
            lastSentBody.current = resolvedBody;
            queuedBody.current = null;
            updateNote.mutate(
              { id: note.id, body: resolvedBody },
              {
                onSuccess: (saved) => {
                  if (saved?.updated_at) baseUpdatedAt.current = saved.updated_at;
                  setLiveBody(resolvedBody);
                  // Remount the editor so it shows the resolved content.
                  setReplaceToken((t) => t + 1);
                },
                onSettled: () => {
                  saveInFlight.current = false;
                  const next = queuedBody.current;
                  queuedBody.current = null;
                  if (next !== null) pushSave(next);
                },
              },
            );
          }}
          onCancel={() => setConflict(null)}
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
