"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  ChevronDown,
  Folder as FolderIcon,
  FolderPlus,
  Plus,
  Search,
  Pencil,
  Trash2,
  ArrowLeftRight,
  MoreHorizontal,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@multica/ui/components/ui/breadcrumb";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@multica/ui/components/ui/sheet";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@multica/ui/components/ui/context-menu";
import { cn } from "@multica/ui/lib/utils";
import {
  artifactFoldersOptions,
  artifactSearchOptions,
} from "@multica/core/artifacts/queries";
import {
  useCreateArtifactFolder,
  useUpdateArtifactFolder,
  useDeleteArtifactFolder,
  useMoveArtifactToFolder,
  useDeleteArtifact,
  useUpdateArtifact,
} from "@multica/core/artifacts/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type {
  Artifact,
  ArtifactFolder,
  ArtifactAuthorType,
} from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { KindIcon, KIND_LABELS } from "./kind-icon";

// ---------------------------------------------------------------------------
// Tree helpers
// ---------------------------------------------------------------------------

interface FolderNode extends ArtifactFolder {
  children: FolderNode[];
}

function buildFolderTree(folders: ArtifactFolder[]): FolderNode[] {
  const byId = new Map<string, FolderNode>();
  folders.forEach((f) => byId.set(f.id, { ...f, children: [] }));
  const roots: FolderNode[] = [];
  byId.forEach((node) => {
    if (node.parent_id && byId.has(node.parent_id)) {
      byId.get(node.parent_id)!.children.push(node);
    } else {
      roots.push(node);
    }
  });
  const sortRecursive = (list: FolderNode[]) => {
    list.sort((a, b) => a.name.localeCompare(b.name));
    list.forEach((n) => sortRecursive(n.children));
  };
  sortRecursive(roots);
  return roots;
}

function pathTo(
  folders: ArtifactFolder[],
  folderId: string | null,
): ArtifactFolder[] {
  if (!folderId) return [];
  const byId = new Map(folders.map((f) => [f.id, f]));
  const out: ArtifactFolder[] = [];
  let cur: ArtifactFolder | undefined = byId.get(folderId);
  while (cur) {
    out.unshift(cur);
    cur = cur.parent_id ? byId.get(cur.parent_id) : undefined;
  }
  return out;
}

function formatBytes(n: number | null | undefined): string {
  if (!n) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

function ScopeBadge({ artifact }: { artifact: Artifact }) {
  const wsPaths = useWorkspacePaths();
  // Prefer the strongest connection: scope-issue > scope-project > origin-issue > workspace.
  if (artifact.issue_id) {
    return (
      <Badge variant="outline" className="text-[10px]">
        <a
          href={wsPaths.issueDetail(artifact.issue_id)}
          onClick={(e) => e.stopPropagation()}
        >
          Issue
        </a>
      </Badge>
    );
  }
  if (artifact.project_id) {
    return (
      <Badge variant="outline" className="text-[10px]">
        <a
          href={wsPaths.projectDetail(artifact.project_id)}
          onClick={(e) => e.stopPropagation()}
        >
          Project
        </a>
      </Badge>
    );
  }
  if (artifact.origin_issue_id) {
    return (
      <Badge variant="outline" className="text-[10px]" title="Created from issue">
        <a
          href={wsPaths.issueDetail(artifact.origin_issue_id)}
          onClick={(e) => e.stopPropagation()}
        >
          From issue
        </a>
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-[10px] text-muted-foreground">
      Workspace
    </Badge>
  );
}

function MobileMeta({ artifact }: { artifact: Artifact }) {
  const { getActorName } = useActorName();
  const parts: string[] = [
    KIND_LABELS[artifact.kind],
    artifact.format.toUpperCase(),
    getActorName(artifact.author_type, artifact.author_id),
    formatDate(artifact.updated_at),
  ];
  return (
    <div className="truncate text-xs text-muted-foreground md:hidden">
      {parts.join(" · ")}
    </div>
  );
}

function AuthorCell({ artifact }: { artifact: Artifact }) {
  const { getActorName, getMemberName } = useActorName();
  const showRequester =
    artifact.requester_user_id &&
    !(
      artifact.author_type === "member" &&
      artifact.author_id === artifact.requester_user_id
    );
  return (
    <div
      className="flex min-w-0 items-center gap-1.5"
      onClick={(e) => e.stopPropagation()}
      title={
        showRequester && artifact.requester_user_id
          ? `Requested by ${getMemberName(artifact.requester_user_id)}`
          : undefined
      }
    >
      <ActorAvatar
        actorType={artifact.author_type}
        actorId={artifact.author_id}
        size={18}
      />
      <div className="min-w-0 flex-1 leading-tight">
        <div className="truncate text-xs text-muted-foreground">
          {getActorName(artifact.author_type, artifact.author_id)}
        </div>
        {showRequester && artifact.requester_user_id && (
          <div className="truncate text-[10px] text-muted-foreground/70">
            for {getMemberName(artifact.requester_user_id)}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// FolderTree
// ---------------------------------------------------------------------------

function FolderTreeItem({
  node,
  depth,
  currentId,
  expanded,
  onToggle,
  onSelect,
  onMoveArtifact,
  onMoveFolder,
  onRename,
  onDelete,
  onNewSubfolder,
}: {
  node: FolderNode;
  depth: number;
  currentId: string | null;
  expanded: Set<string>;
  onToggle: (id: string) => void;
  onSelect: (id: string | null) => void;
  onMoveArtifact: (artifactId: string, folderId: string | null) => void;
  onMoveFolder: (folderId: string, parentId: string | null) => void;
  onRename: (folder: ArtifactFolder) => void;
  onDelete: (folder: ArtifactFolder) => void;
  onNewSubfolder: (parentId: string) => void;
}) {
  const isOpen = expanded.has(node.id);
  const isActive = currentId === node.id;
  const [dragOver, setDragOver] = React.useState(false);
  return (
    <div>
      <ContextMenu>
        <ContextMenuTrigger
          render={<div />}
          draggable
          onDragStart={(e) => {
            e.dataTransfer.setData("text/multica-folder", node.id);
            e.dataTransfer.effectAllowed = "move";
          }}
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => {
            e.preventDefault();
            setDragOver(false);
            const artifactId = e.dataTransfer.getData("text/multica-artifact");
            if (artifactId) {
              onMoveArtifact(artifactId, node.id);
              return;
            }
            const folderId = e.dataTransfer.getData("text/multica-folder");
            if (folderId && folderId !== node.id) {
              onMoveFolder(folderId, node.id);
            }
          }}
          className={cn(
            "group flex cursor-grab items-center gap-1 rounded px-2 py-1 text-left text-sm hover:bg-accent active:cursor-grabbing",
            isActive && "bg-accent font-medium",
            dragOver && "bg-accent/70 ring-1 ring-primary/50",
          )}
        >
          <button
            type="button"
            onClick={() => onSelect(node.id)}
            className="flex w-full items-center gap-1 text-left"
            style={{ paddingLeft: depth * 12 }}
          >
            {node.children.length > 0 ? (
              <span
                role="button"
                tabIndex={-1}
                onClick={(e) => {
                  e.stopPropagation();
                  onToggle(node.id);
                }}
                className="flex size-4 items-center justify-center text-muted-foreground"
              >
                {isOpen ? (
                  <ChevronDown className="size-3.5" />
                ) : (
                  <ChevronRight className="size-3.5" />
                )}
              </span>
            ) : (
              <span className="size-4" />
            )}
            <FolderIcon className="size-4 text-muted-foreground" />
            <span className="flex-1 truncate">{node.name}</span>
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-5 shrink-0 text-muted-foreground opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100"
                  aria-label={`Actions for ${node.name}`}
                />
              }
              onClick={(e) => e.stopPropagation()}
            >
              <MoreHorizontal className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => onRename(node)}>
                <Pencil className="mr-2 size-3.5" /> Rename
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onNewSubfolder(node.id)}>
                <FolderPlus className="mr-2 size-3.5" /> New subfolder
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="text-destructive"
                onSelect={() => onDelete(node)}
              >
                <Trash2 className="mr-2 size-3.5" /> Delete folder
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onSelect={() => onRename(node)}>
            <Pencil className="mr-2 size-3.5" /> Rename
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => onNewSubfolder(node.id)}>
            <FolderPlus className="mr-2 size-3.5" /> New subfolder
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem
            className="text-destructive"
            onSelect={() => onDelete(node)}
          >
            <Trash2 className="mr-2 size-3.5" /> Delete folder
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {isOpen && node.children.length > 0 && (
        <div>
          {node.children.map((c) => (
            <FolderTreeItem
              key={c.id}
              node={c}
              depth={depth + 1}
              currentId={currentId}
              expanded={expanded}
              onToggle={onToggle}
              onSelect={onSelect}
              onMoveArtifact={onMoveArtifact}
              onMoveFolder={onMoveFolder}
              onRename={onRename}
              onDelete={onDelete}
              onNewSubfolder={onNewSubfolder}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// New folder dialog
// ---------------------------------------------------------------------------

function NewFolderDialog({
  open,
  onOpenChange,
  parentId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  parentId: string | null;
}) {
  const [name, setName] = React.useState("");
  const create = useCreateArtifactFolder();

  React.useEffect(() => {
    if (!open) setName("");
  }, [open]);

  const handleCreate = async () => {
    if (!name.trim()) return;
    await create.mutateAsync({ name: name.trim(), parent_id: parentId });
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New folder</DialogTitle>
          <DialogDescription>
            Folders group documents — they're independent of project/issue scope.
          </DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Folder name"
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              handleCreate();
            }
          }}
        />
        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={create.isPending}
          >
            Cancel
          </Button>
          <Button
            onClick={handleCreate}
            disabled={!name.trim() || create.isPending}
          >
            {create.isPending ? "Creating…" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// File manager page
// ---------------------------------------------------------------------------

export interface FileManagerPageProps {
  /** Selected folder from query string (`?folder=<id>`). */
  initialFolderId?: string | null;
}

export function FileManagerPage({ initialFolderId }: FileManagerPageProps = {}) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();

  const [folderId, setFolderId] = React.useState<string | null>(
    initialFolderId ?? null,
  );
  const [expanded, setExpanded] = React.useState<Set<string>>(new Set());
  const [query, setQuery] = React.useState("");
  const [newFolderOpen, setNewFolderOpen] = React.useState(false);
  // Parent for the "New folder" dialog: null = current folder context,
  // otherwise an explicit folder id (e.g. when invoked from a tree item).
  const [newFolderParent, setNewFolderParent] = React.useState<
    string | null | undefined
  >(undefined);
  const [renameTarget, setRenameTarget] = React.useState<ArtifactFolder | null>(
    null,
  );
  const [renameDraft, setRenameDraft] = React.useState("");
  const [renameArtifactTarget, setRenameArtifactTarget] =
    React.useState<Artifact | null>(null);
  const [renameArtifactDraft, setRenameArtifactDraft] = React.useState("");
  const [deleteFolderTarget, setDeleteFolderTarget] =
    React.useState<ArtifactFolder | null>(null);
  const [deleteArtifactTarget, setDeleteArtifactTarget] =
    React.useState<Artifact | null>(null);
  const [selectedFolders, setSelectedFolders] = React.useState<Set<string>>(
    new Set(),
  );
  const [selectedArtifacts, setSelectedArtifacts] = React.useState<Set<string>>(
    new Set(),
  );
  const [batchDeleteOpen, setBatchDeleteOpen] = React.useState(false);
  const [authorFilter, setAuthorFilter] = React.useState<
    "all" | ArtifactAuthorType
  >("all");
  const [foldersSheetOpen, setFoldersSheetOpen] = React.useState(false);

  React.useEffect(() => {
    setFolderId(initialFolderId ?? null);
  }, [initialFolderId]);

  // Selection is per-folder-view; navigating away clears it.
  React.useEffect(() => {
    setSelectedFolders(new Set());
    setSelectedArtifacts(new Set());
  }, [folderId]);

  const { data: folders = [] } = useQuery(artifactFoldersOptions(wsId));
  const { data: allArtifacts = [], isLoading } = useQuery(
    artifactSearchOptions(wsId, {
      q: query.trim() || undefined,
      author_type: authorFilter === "all" ? undefined : authorFilter,
      limit: 200,
    }),
  );

  const updateFolder = useUpdateArtifactFolder();
  const deleteFolder = useDeleteArtifactFolder();
  const moveArtifact = useMoveArtifactToFolder();
  const deleteArtifact = useDeleteArtifact();
  const updateArtifact = useUpdateArtifact();

  const tree = React.useMemo(() => buildFolderTree(folders), [folders]);
  const breadcrumbs = React.useMemo(
    () => pathTo(folders, folderId),
    [folders, folderId],
  );

  // Subfolders inside the current folder.
  const childFolders = React.useMemo(() => {
    if (folderId === null) return tree;
    const find = (nodes: FolderNode[]): FolderNode | undefined => {
      for (const n of nodes) {
        if (n.id === folderId) return n;
        const sub = find(n.children);
        if (sub) return sub;
      }
      return undefined;
    };
    return find(tree)?.children ?? [];
  }, [tree, folderId]);

  // Artifacts in the current folder. Server returns all artifacts (no folder
  // filter on the search endpoint), so filter client-side. The cap of 200
  // is generous for early use; we'll add a server filter if needed.
  const artifacts = React.useMemo(() => {
    return allArtifacts.filter((a) => (a.folder_id ?? null) === folderId);
  }, [allArtifacts, folderId]);

  const toggleFolder = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const handleMoveArtifact = (artifactId: string, target: string | null) => {
    if (artifactId) {
      moveArtifact.mutate({ id: artifactId, data: { folder_id: target } });
    }
  };

  // Move a folder by changing its parent. Backend rejects self-parenting; we
  // skip the trivial no-op here (descendant cycles are still server-checked).
  const handleMoveFolder = (folderId: string, parentId: string | null) => {
    if (folderId && folderId !== parentId) {
      updateFolder.mutate({
        id: folderId,
        data: { parent_id: parentId ?? "" },
      });
    }
  };

  const openNewFolderDialog = (parent?: string | null) => {
    setNewFolderParent(parent);
    setNewFolderOpen(true);
  };

  const handleRename = async () => {
    if (!renameTarget || !renameDraft.trim()) return;
    await updateFolder.mutateAsync({
      id: renameTarget.id,
      data: { name: renameDraft.trim() },
    });
    setRenameTarget(null);
  };

  const handleDeleteFolder = async () => {
    if (!deleteFolderTarget) return;
    await deleteFolder.mutateAsync({ id: deleteFolderTarget.id });
    if (folderId === deleteFolderTarget.id) setFolderId(null);
    setDeleteFolderTarget(null);
  };

  const handleRenameArtifact = async () => {
    if (!renameArtifactTarget || !renameArtifactDraft.trim()) return;
    await updateArtifact.mutateAsync({
      id: renameArtifactTarget.id,
      data: { title: renameArtifactDraft.trim() },
    });
    setRenameArtifactTarget(null);
  };

  const handleDeleteArtifact = async () => {
    if (!deleteArtifactTarget) return;
    await deleteArtifact.mutateAsync(deleteArtifactTarget);
    setDeleteArtifactTarget(null);
  };

  // ---- Selection helpers ----
  const totalSelected = selectedFolders.size + selectedArtifacts.size;
  const allInViewSelected =
    childFolders.length + artifacts.length > 0 &&
    selectedFolders.size === childFolders.length &&
    selectedArtifacts.size === artifacts.length;

  const toggleSelectFolder = (id: string) => {
    setSelectedFolders((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const toggleSelectArtifact = (id: string) => {
    setSelectedArtifacts((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const selectAllInView = () => {
    if (allInViewSelected) {
      setSelectedFolders(new Set());
      setSelectedArtifacts(new Set());
    } else {
      setSelectedFolders(new Set(childFolders.map((f) => f.id)));
      setSelectedArtifacts(new Set(artifacts.map((a) => a.id)));
    }
  };
  const clearSelection = () => {
    setSelectedFolders(new Set());
    setSelectedArtifacts(new Set());
  };

  const handleBatchMove = async (target: string | null) => {
    const promises: Promise<unknown>[] = [];
    selectedArtifacts.forEach((id) => {
      promises.push(
        moveArtifact.mutateAsync({ id, data: { folder_id: target } }),
      );
    });
    selectedFolders.forEach((id) => {
      // Don't move a folder into itself or one of its descendants. Server
      // would reject self-parenting; descendant-cycle is not currently
      // detected — for v1 we just let the server return an error.
      if (target !== id) {
        promises.push(
          updateFolder.mutateAsync({ id, data: { parent_id: target } }),
        );
      }
    });
    await Promise.allSettled(promises);
    clearSelection();
  };

  const handleBatchDelete = async () => {
    const promises: Promise<unknown>[] = [];
    selectedArtifacts.forEach((id) => {
      const a = artifacts.find((x) => x.id === id);
      if (a) promises.push(deleteArtifact.mutateAsync(a));
    });
    selectedFolders.forEach((id) => {
      promises.push(deleteFolder.mutateAsync({ id }));
    });
    await Promise.allSettled(promises);
    clearSelection();
    setBatchDeleteOpen(false);
  };

  // Folder tree shared between desktop sidebar and mobile sheet.
  const renderFolderTree = (onPick?: () => void) => (
    <div className="flex flex-1 flex-col">
      <div className="flex items-center justify-between border-b border-border px-3 py-2">
        <span className="text-xs font-medium uppercase text-muted-foreground">
          Folders
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="size-6"
          onClick={() => openNewFolderDialog()}
          title="New folder"
        >
          <FolderPlus className="size-3.5" />
        </Button>
      </div>
      <div className="flex-1 overflow-y-auto p-1">
        <button
          type="button"
          onClick={() => {
            setFolderId(null);
            onPick?.();
          }}
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            const artifactId = e.dataTransfer.getData("text/multica-artifact");
            if (artifactId) {
              handleMoveArtifact(artifactId, null);
              return;
            }
            const movedFolderId = e.dataTransfer.getData("text/multica-folder");
            if (movedFolderId) handleMoveFolder(movedFolderId, null);
          }}
          className={cn(
            "flex w-full items-center gap-1 rounded px-2 py-1 text-left text-sm hover:bg-accent",
            folderId === null && "bg-accent font-medium",
          )}
        >
          <span className="size-4" />
          <FolderIcon className="size-4 text-muted-foreground" />
          <span>All documents</span>
        </button>
        {tree.map((n) => (
          <FolderTreeItem
            key={n.id}
            node={n}
            depth={0}
            currentId={folderId}
            expanded={expanded}
            onToggle={toggleFolder}
            onSelect={(id) => {
              setFolderId(id);
              onPick?.();
            }}
            onMoveArtifact={handleMoveArtifact}
            onMoveFolder={handleMoveFolder}
            onRename={(folder) => {
              setRenameTarget(folder);
              setRenameDraft(folder.name);
            }}
            onDelete={setDeleteFolderTarget}
            onNewSubfolder={(parent) => openNewFolderDialog(parent)}
          />
        ))}
      </div>
    </div>
  );

  return (
    <div className="flex h-full w-full">
      {/* Desktop sidebar */}
      <aside className="hidden w-64 shrink-0 flex-col border-r border-border bg-card/30 md:flex">
        {renderFolderTree()}
      </aside>

      {/* Main: breadcrumbs, toolbar, list */}
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {/* Toolbar — single row on desktop, two-row stack on mobile.
            md:flex-wrap so rows fall to a second line when the main panel is
            narrow (e.g. with the folder sidebar visible at ~768–900px). */}
        <div className="flex flex-col gap-2 border-b border-border px-4 py-2 md:flex-row md:flex-wrap md:items-center md:justify-between md:gap-3 md:px-6">
          {/* Row 1: folder access + breadcrumb + primary action */}
          <div className="flex min-w-0 items-center gap-2">
            <Sheet open={foldersSheetOpen} onOpenChange={setFoldersSheetOpen}>
              <SheetTrigger
                render={
                  <Button
                    variant="outline"
                    size="sm"
                    className="md:hidden"
                    aria-label="Browse folders"
                  />
                }
              >
                <FolderIcon className="size-4" />
              </SheetTrigger>
              <SheetContent side="left" className="flex w-full max-w-[85vw] sm:w-72 sm:max-w-[18rem] flex-col p-0">
                <SheetHeader className="sr-only">
                  <SheetTitle>Folders</SheetTitle>
                </SheetHeader>
                {renderFolderTree(() => setFoldersSheetOpen(false))}
              </SheetContent>
            </Sheet>
            <Breadcrumb className="min-w-0 flex-1">
              <BreadcrumbList className="flex-nowrap">
                <BreadcrumbItem>
                  {folderId === null ? (
                    <BreadcrumbPage className="truncate">All documents</BreadcrumbPage>
                  ) : (
                    <BreadcrumbLink
                      onClick={(e) => {
                        e.preventDefault();
                        setFolderId(null);
                      }}
                      className="cursor-pointer truncate"
                    >
                      All documents
                    </BreadcrumbLink>
                  )}
                </BreadcrumbItem>
                {breadcrumbs.map((f, i) => (
                  <React.Fragment key={f.id}>
                    <BreadcrumbSeparator />
                    <BreadcrumbItem className="min-w-0">
                      {i === breadcrumbs.length - 1 ? (
                        <BreadcrumbPage className="truncate">{f.name}</BreadcrumbPage>
                      ) : (
                        <BreadcrumbLink
                          onClick={(e) => {
                            e.preventDefault();
                            setFolderId(f.id);
                          }}
                          className="cursor-pointer truncate"
                        >
                          {f.name}
                        </BreadcrumbLink>
                      )}
                    </BreadcrumbItem>
                  </React.Fragment>
                ))}
              </BreadcrumbList>
            </Breadcrumb>
            {/* Primary action stays on the breadcrumb row on mobile so it's
                always reachable above the search bar. */}
            <Button
              size="sm"
              onClick={() => router.push(wsPaths.documentNew(folderId))}
              className="md:hidden"
            >
              <Plus className="mr-1 size-4" /> New
            </Button>
          </div>
          {/* Row 2: search + author + actions */}
          <div className="flex items-center gap-2 md:flex-shrink-0">
            <div className="relative flex-1 md:flex-initial">
              <Search className="absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search documents…"
                className="h-8 w-full pl-7 md:w-56"
              />
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="outline" size="sm" />}
                aria-label="Filter by author"
              >
                <span className="hidden sm:inline">
                  Author:{" "}
                  {authorFilter === "all"
                    ? "Anyone"
                    : authorFilter === "agent"
                      ? "Agents"
                      : "Members"}
                </span>
                <span className="sm:hidden">
                  {authorFilter === "all"
                    ? "All"
                    : authorFilter === "agent"
                      ? "Agents"
                      : "Members"}
                </span>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onSelect={() => setAuthorFilter("all")}>
                  Anyone
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setAuthorFilter("member")}>
                  Members only
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setAuthorFilter("agent")}>
                  Agents only
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              size="sm"
              variant="outline"
              onClick={() => openNewFolderDialog()}
            >
              <FolderPlus className="mr-1 size-4" />
              <span className="hidden sm:inline">New folder</span>
              <span className="sm:hidden">Folder</span>
            </Button>
            <Button
              size="sm"
              onClick={() => router.push(wsPaths.documentNew(folderId))}
              className="hidden md:inline-flex"
            >
              <Plus className="mr-1 size-4" /> New document
            </Button>
          </div>
        </div>

        {totalSelected > 0 && (
          <div className="flex flex-wrap items-center gap-2 border-b border-border bg-muted/40 px-4 py-1.5 text-sm md:px-6">
            <span className="text-muted-foreground">
              {totalSelected} selected
            </span>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="ghost" size="sm" />}
              >
                <ArrowLeftRight className="mr-1 size-4" /> Move to
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onSelect={() => handleBatchMove(null)}>
                  All documents (root)
                </DropdownMenuItem>
                {folders.length > 0 && <DropdownMenuSeparator />}
                {folders.map((f) => (
                  <DropdownMenuItem
                    key={f.id}
                    onSelect={() => handleBatchMove(f.id)}
                  >
                    <FolderIcon className="mr-2 size-3.5" />
                    {f.name}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive hover:text-destructive"
              onClick={() => setBatchDeleteOpen(true)}
            >
              <Trash2 className="mr-1 size-4" /> Delete
            </Button>
            <Button variant="ghost" size="sm" onClick={clearSelection}>
              Cancel
            </Button>
          </div>
        )}

        <div className="flex-1 overflow-auto">
          {isLoading ? (
            <div className="px-6 py-6 text-sm text-muted-foreground">Loading…</div>
          ) : childFolders.length === 0 && artifacts.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-6 py-16 text-center text-sm text-muted-foreground">
              <FolderIcon className="size-8 opacity-40" />
              <span>This folder is empty.</span>
              <Button
                size="sm"
                variant="outline"
                onClick={() => router.push(wsPaths.documentNew(folderId))}
              >
                <Plus className="mr-1 size-4" /> New document
              </Button>
            </div>
          ) : (
            <div className="flex flex-col text-sm md:grid md:min-w-[820px] md:grid-cols-[56px_minmax(0,1fr)_100px_80px_160px_120px_110px_44px]">
              {/* Mobile-only header: just select-all */}
              <div className="flex items-center justify-between border-b border-border bg-muted/20 px-4 py-1.5 text-xs font-medium uppercase text-muted-foreground md:hidden">
                <label className="flex cursor-pointer items-center gap-2">
                  <Checkbox
                    checked={allInViewSelected}
                    onCheckedChange={selectAllInView}
                    aria-label="Select all"
                  />
                  Select all
                </label>
                <span className="normal-case">
                  {childFolders.length + artifacts.length} item
                  {childFolders.length + artifacts.length === 1 ? "" : "s"}
                </span>
              </div>
              {/* Desktop table header */}
              <div className="col-span-8 hidden grid-cols-subgrid border-b border-border bg-muted/20 px-6 py-1 text-xs font-medium uppercase text-muted-foreground md:grid">
                <div className="flex items-center">
                  <Checkbox
                    checked={allInViewSelected}
                    onCheckedChange={selectAllInView}
                    aria-label="Select all"
                  />
                </div>
                <div>Name</div>
                <div>Kind</div>
                <div>Format</div>
                <div>Author</div>
                <div>Linked to</div>
                <div>Modified</div>
                <div />
              </div>
              {childFolders.map((f) => (
                <ContextMenu key={f.id}>
                  <ContextMenuTrigger
                    draggable
                    onDragStart={(e) => {
                      e.dataTransfer.setData("text/multica-folder", f.id);
                      e.dataTransfer.effectAllowed = "move";
                    }}
                    onClick={() => setFolderId(f.id)}
                    onDragOver={(e) => e.preventDefault()}
                    onDrop={(e) => {
                      e.preventDefault();
                      const artifactId = e.dataTransfer.getData(
                        "text/multica-artifact",
                      );
                      if (artifactId) {
                        handleMoveArtifact(artifactId, f.id);
                        return;
                      }
                      const movedFolderId = e.dataTransfer.getData(
                        "text/multica-folder",
                      );
                      if (movedFolderId && movedFolderId !== f.id) {
                        handleMoveFolder(movedFolderId, f.id);
                      }
                    }}
                    className="flex cursor-pointer items-center gap-3 border-b border-border/50 px-4 py-2.5 text-left hover:bg-accent/50 md:col-span-8 md:grid md:grid-cols-subgrid md:gap-0 md:border-b-0 md:px-6 md:py-2"
                  >
                    <div
                      className="flex items-center"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <Checkbox
                        checked={selectedFolders.has(f.id)}
                        onCheckedChange={() => toggleSelectFolder(f.id)}
                        aria-label={`Select ${f.name}`}
                      />
                    </div>
                    <div className="flex min-w-0 flex-1 items-center gap-2">
                      <FolderIcon className="size-4 shrink-0 text-muted-foreground" />
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium">{f.name}</div>
                        <div className="truncate text-xs text-muted-foreground md:hidden">
                          Folder · {formatDate(f.updated_at)}
                        </div>
                      </div>
                    </div>
                    <div className="hidden text-xs text-muted-foreground md:block">
                      Folder
                    </div>
                    <div className="hidden md:block" />
                    <div className="hidden md:block" />
                    <div className="hidden md:block" />
                    <div className="hidden text-xs text-muted-foreground md:block">
                      {formatDate(f.updated_at)}
                    </div>
                    <div
                      className="flex items-center justify-end"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-7 text-muted-foreground"
                              aria-label={`Actions for ${f.name}`}
                            />
                          }
                        >
                          <MoreHorizontal className="size-4" />
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onSelect={() => {
                              setRenameTarget(f);
                              setRenameDraft(f.name);
                            }}
                          >
                            <Pencil className="mr-2 size-3.5" /> Rename
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="text-destructive"
                            onSelect={() => setDeleteFolderTarget(f)}
                          >
                            <Trash2 className="mr-2 size-3.5" /> Delete folder
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem
                      onSelect={() => {
                        setRenameTarget(f);
                        setRenameDraft(f.name);
                      }}
                    >
                      <Pencil className="mr-2 size-3.5" /> Rename
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem
                      className="text-destructive"
                      onSelect={() => setDeleteFolderTarget(f)}
                    >
                      <Trash2 className="mr-2 size-3.5" /> Delete folder
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              ))}
              {artifacts.map((a) => (
                <ContextMenu key={a.id}>
                  <ContextMenuTrigger
                    draggable
                    onDragStart={(e) => {
                      e.dataTransfer.setData("text/multica-artifact", a.id);
                      e.dataTransfer.effectAllowed = "move";
                    }}
                    onClick={() => router.push(wsPaths.documentDetail(a.id))}
                    onDoubleClick={() =>
                      router.push(wsPaths.documentEdit(a.id))
                    }
                    className="flex cursor-pointer items-center gap-3 border-b border-border/50 px-4 py-2.5 text-left hover:bg-accent/50 md:col-span-8 md:grid md:grid-cols-subgrid md:items-center md:gap-0 md:border-b-0 md:px-6 md:py-2"
                  >
                    <div
                      className="flex items-center"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <Checkbox
                        checked={selectedArtifacts.has(a.id)}
                        onCheckedChange={() => toggleSelectArtifact(a.id)}
                        aria-label={`Select ${a.title}`}
                      />
                    </div>
                    <div className="flex min-w-0 flex-1 items-center gap-2">
                      <KindIcon
                        kind={a.kind}
                        className="size-4 shrink-0 text-muted-foreground"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium">{a.title}</div>
                        <MobileMeta artifact={a} />
                      </div>
                    </div>
                    <div className="hidden text-xs text-muted-foreground md:block">
                      {KIND_LABELS[a.kind]}
                    </div>
                    <div className="hidden text-xs uppercase text-muted-foreground md:block">
                      {a.format}
                      {a.format === "pdf" && a.file_size_bytes
                        ? ` · ${formatBytes(a.file_size_bytes)}`
                        : ""}
                    </div>
                    <div className="hidden md:block">
                      <AuthorCell artifact={a} />
                    </div>
                    <div className="hidden md:block">
                      <ScopeBadge artifact={a} />
                    </div>
                    <div className="hidden text-xs text-muted-foreground md:block">
                      {formatDate(a.updated_at)}
                    </div>
                    <div
                      className="flex items-center justify-end"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-7 text-muted-foreground"
                              aria-label={`Actions for ${a.title}`}
                            />
                          }
                        >
                          <MoreHorizontal className="size-4" />
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onSelect={() => {
                              setRenameArtifactTarget(a);
                              setRenameArtifactDraft(a.title);
                            }}
                          >
                            <Pencil className="mr-2 size-3.5" /> Rename
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            onSelect={() =>
                              router.push(wsPaths.documentEdit(a.id))
                            }
                          >
                            <Pencil className="mr-2 size-3.5" /> Edit body
                          </DropdownMenuItem>
                          <DropdownMenuSub>
                            <DropdownMenuSubTrigger>
                              <ArrowLeftRight className="mr-2 size-3.5" /> Move
                              to
                            </DropdownMenuSubTrigger>
                            <DropdownMenuSubContent>
                              <DropdownMenuItem
                                onSelect={() => handleMoveArtifact(a.id, null)}
                              >
                                All documents (root)
                              </DropdownMenuItem>
                              {folders.length > 0 && <DropdownMenuSeparator />}
                              {folders.map((f) => (
                                <DropdownMenuItem
                                  key={f.id}
                                  onSelect={() =>
                                    handleMoveArtifact(a.id, f.id)
                                  }
                                >
                                  <FolderIcon className="mr-2 size-3.5" />
                                  {f.name}
                                </DropdownMenuItem>
                              ))}
                            </DropdownMenuSubContent>
                          </DropdownMenuSub>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="text-destructive"
                            onSelect={() => setDeleteArtifactTarget(a)}
                          >
                            <Trash2 className="mr-2 size-3.5" /> Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem
                      onSelect={() => {
                        setRenameArtifactTarget(a);
                        setRenameArtifactDraft(a.title);
                      }}
                    >
                      <Pencil className="mr-2 size-3.5" /> Rename
                    </ContextMenuItem>
                    <ContextMenuItem
                      onSelect={() =>
                        router.push(wsPaths.documentEdit(a.id))
                      }
                    >
                      <Pencil className="mr-2 size-3.5" /> Edit body
                    </ContextMenuItem>
                    <ContextMenuSub>
                      <ContextMenuSubTrigger>
                        <ArrowLeftRight className="mr-2 size-3.5" /> Move to
                      </ContextMenuSubTrigger>
                      <ContextMenuSubContent>
                        <ContextMenuItem
                          onSelect={() => handleMoveArtifact(a.id, null)}
                        >
                          All documents (root)
                        </ContextMenuItem>
                        {folders.length > 0 && <ContextMenuSeparator />}
                        {folders.map((f) => (
                          <ContextMenuItem
                            key={f.id}
                            onSelect={() => handleMoveArtifact(a.id, f.id)}
                          >
                            <FolderIcon className="mr-2 size-3.5" />
                            {f.name}
                          </ContextMenuItem>
                        ))}
                      </ContextMenuSubContent>
                    </ContextMenuSub>
                    <ContextMenuSeparator />
                    <ContextMenuItem
                      className="text-destructive"
                      onSelect={() => setDeleteArtifactTarget(a)}
                    >
                      <Trash2 className="mr-2 size-3.5" /> Delete
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              ))}
            </div>
          )}
        </div>
      </main>

      <NewFolderDialog
        open={newFolderOpen}
        onOpenChange={(open) => {
          setNewFolderOpen(open);
          if (!open) setNewFolderParent(undefined);
        }}
        parentId={newFolderParent === undefined ? folderId : newFolderParent}
      />

      <Dialog
        open={Boolean(renameTarget)}
        onOpenChange={(open) => !open && setRenameTarget(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename folder</DialogTitle>
          </DialogHeader>
          <Input
            autoFocus
            value={renameDraft}
            onChange={(e) => setRenameDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleRename();
              }
            }}
          />
          <DialogFooter>
            <Button variant="ghost" onClick={() => setRenameTarget(null)}>
              Cancel
            </Button>
            <Button
              onClick={handleRename}
              disabled={!renameDraft.trim() || updateFolder.isPending}
            >
              {updateFolder.isPending ? "Saving…" : "Rename"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={Boolean(deleteFolderTarget)}
        onOpenChange={(open) => !open && setDeleteFolderTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete folder?</AlertDialogTitle>
            <AlertDialogDescription>
              Deleting &ldquo;{deleteFolderTarget?.name}&rdquo; also deletes any
              subfolders. Documents inside move back to the root.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteFolder}
              disabled={deleteFolder.isPending}
            >
              {deleteFolder.isPending ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog
        open={Boolean(renameArtifactTarget)}
        onOpenChange={(open) => !open && setRenameArtifactTarget(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename document</DialogTitle>
          </DialogHeader>
          <Input
            autoFocus
            value={renameArtifactDraft}
            onChange={(e) => setRenameArtifactDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleRenameArtifact();
              }
            }}
          />
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setRenameArtifactTarget(null)}
            >
              Cancel
            </Button>
            <Button
              onClick={handleRenameArtifact}
              disabled={
                !renameArtifactDraft.trim() || updateArtifact.isPending
              }
            >
              {updateArtifact.isPending ? "Saving…" : "Rename"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={Boolean(deleteArtifactTarget)}
        onOpenChange={(open) => !open && setDeleteArtifactTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete document?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently removes &ldquo;{deleteArtifactTarget?.title}
              &rdquo;. Cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteArtifact}
              disabled={deleteArtifact.isPending}
            >
              {deleteArtifact.isPending ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={batchDeleteOpen} onOpenChange={setBatchDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {totalSelected} item(s)?</AlertDialogTitle>
            <AlertDialogDescription>
              {selectedFolders.size > 0 && selectedArtifacts.size > 0
                ? `${selectedFolders.size} folder(s) and ${selectedArtifacts.size} document(s) will be deleted.`
                : selectedFolders.size > 0
                  ? `${selectedFolders.size} folder(s) will be deleted. Documents inside move back to the root.`
                  : `${selectedArtifacts.size} document(s) will be deleted.`}
              {" "}This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleBatchDelete}
              disabled={deleteFolder.isPending || deleteArtifact.isPending}
            >
              {deleteFolder.isPending || deleteArtifact.isPending
                ? "Deleting…"
                : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

