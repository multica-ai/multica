"use client";

// FIR-1412: the folder tree sidebar for the Skills and Autopilots list pages.
// Holds all folder UI — tree + create/rename/delete (incl. sub-folders) and an
// "Add items" multi-select dialog that files skills/autopilots into a folder.
// The host list page only renders this beside its list and filters by the
// selected folder.

import { useMemo, useState, type DragEvent } from "react";
import {
  ChevronDown,
  ChevronRight,
  FolderPlus,
  Inbox,
  Layers,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { cn } from "@multica/ui/lib/utils";
import {
  Dialog,
  DialogContent,
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  useCreateEntityFolder,
  useDeleteEntityFolder,
  useSetEntityFolderItem,
  useUpdateEntityFolder,
} from "../mutations";
import {
  hasEntityFolderDragData,
  readEntityFolderDragData,
} from "../dnd";
import type { EntityFolder, EntityFolderKind } from "../types";
import type { EntityFolderViewItem } from "./use-entity-folder-view";

// Selection sentinels for the two virtual rows above the folder tree.
export const SELECT_ALL = "all";
export const SELECT_UNFILED = "unfiled";

interface FolderNode extends EntityFolder {
  children: FolderNode[];
}

function buildFolderTree(folders: EntityFolder[]): FolderNode[] {
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

interface EntityFolderSidebarProps {
  kind: EntityFolderKind;
  folders: EntityFolder[];
  items: EntityFolderViewItem[];
  /** itemId -> folderId for filed items. */
  membership: Map<string, string>;
  selected: string;
  onSelect: (sel: string) => void;
  /** Human label for the items, e.g. "skills" / "autopilots". */
  itemNoun: string;
  /**
   * FIR-1772: rendered inside a mobile Sheet drawer instead of inline. Drops
   * the fixed `w-56` / `border-r` desktop chrome and fills the drawer. When
   * false (default) the root is hidden under `md` so the inline sidebar never
   * crushes the list on phones — mobile reaches it via the Sheet trigger.
   */
  inSheet?: boolean;
}

export function EntityFolderSidebar({
  kind,
  folders,
  items,
  membership,
  selected,
  onSelect,
  itemNoun,
  inSheet = false,
}: EntityFolderSidebarProps) {
  const tree = useMemo(() => buildFolderTree(folders), [folders]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Direct item count per folder id.
  const counts = useMemo(() => {
    const map = new Map<string, number>();
    membership.forEach((folderId) => {
      map.set(folderId, (map.get(folderId) ?? 0) + 1);
    });
    return map;
  }, [membership]);
  const unfiledCount = useMemo(
    () => items.filter((it) => !membership.has(it.id)).length,
    [items, membership],
  );

  const createFolder = useCreateEntityFolder();
  const updateFolder = useUpdateEntityFolder(kind);
  const deleteFolder = useDeleteEntityFolder(kind);
  const setItem = useSetEntityFolderItem();

  // Drag-and-drop: which drop target (folder id or SELECT_UNFILED) the dragged
  // row is currently hovering, for the highlight.
  const [dropTarget, setDropTarget] = useState<string | null>(null);

  const dropHandlers = (folderId: string | null, key: string) => ({
    onDragOver: (e: DragEvent) => {
      if (!hasEntityFolderDragData(e)) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      if (dropTarget !== key) setDropTarget(key);
    },
    onDragLeave: () =>
      setDropTarget((cur) => (cur === key ? null : cur)),
    onDrop: (e: DragEvent) => {
      e.preventDefault();
      setDropTarget(null);
      const itemId = readEntityFolderDragData(e, kind);
      if (!itemId) return;
      // No-op if it's already where it was dropped.
      const current = membership.get(itemId) ?? null;
      if (current === folderId) return;
      setItem.mutate({ kind, item_id: itemId, folder_id: folderId });
    },
  });

  // Create / rename dialog state.
  const [nameDialog, setNameDialog] = useState<
    | { mode: "create"; parentId: string | null }
    | { mode: "rename"; folder: EntityFolder }
    | null
  >(null);
  const [nameDraft, setNameDraft] = useState("");

  const [deleteTarget, setDeleteTarget] = useState<EntityFolder | null>(null);
  const [addTarget, setAddTarget] = useState<EntityFolder | null>(null);

  const openCreate = (parentId: string | null) => {
    setNameDraft("");
    setNameDialog({ mode: "create", parentId });
    if (parentId) {
      setExpanded((prev) => new Set(prev).add(parentId));
    }
  };
  const openRename = (folder: EntityFolder) => {
    setNameDraft(folder.name);
    setNameDialog({ mode: "rename", folder });
  };

  const submitName = () => {
    const name = nameDraft.trim();
    if (!name || !nameDialog) return;
    if (nameDialog.mode === "create") {
      createFolder.mutate({ kind, name, parent_id: nameDialog.parentId });
    } else {
      updateFolder.mutate({ id: nameDialog.folder.id, input: { name } });
    }
    setNameDialog(null);
  };

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const renderNode = (node: FolderNode, depth: number) => {
    const isOpen = expanded.has(node.id);
    const hasChildren = node.children.length > 0;
    const count = counts.get(node.id) ?? 0;
    return (
      <div key={node.id}>
        <div
          className={cn(
            "group/folder flex items-center gap-1 rounded-md pr-1 text-sm",
            selected === node.id
              ? "bg-accent text-accent-foreground"
              : "hover:bg-accent/50",
            dropTarget === node.id &&
              "bg-primary/10 ring-1 ring-inset ring-primary/40",
          )}
          style={{ paddingLeft: `${depth * 12 + 4}px` }}
          {...dropHandlers(node.id, node.id)}
        >
          <button
            type="button"
            className="flex h-6 w-5 shrink-0 items-center justify-center text-muted-foreground"
            onClick={() => (hasChildren ? toggle(node.id) : undefined)}
            aria-label={hasChildren ? "Toggle folder" : undefined}
          >
            {hasChildren ? (
              isOpen ? (
                <ChevronDown className="h-3.5 w-3.5" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5" />
              )
            ) : null}
          </button>
          <button
            type="button"
            className="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left"
            onClick={() => onSelect(node.id)}
          >
            <span className="min-w-0 flex-1 truncate">{node.name}</span>
            {count > 0 && (
              <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground/70">
                {count}
              </span>
            )}
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground/60 transition-colors hover:bg-accent hover:text-foreground"
                  aria-label="Folder actions"
                >
                  <MoreHorizontal className="h-3.5 w-3.5" />
                </button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setAddTarget(node)}>
                <Plus className="h-3.5 w-3.5" />
                Add {itemNoun}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => openCreate(node.id)}>
                <FolderPlus className="h-3.5 w-3.5" />
                New sub-folder
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => openRename(node)}>
                <Pencil className="h-3.5 w-3.5" />
                Rename
              </DropdownMenuItem>
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setDeleteTarget(node)}
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        {isOpen &&
          hasChildren &&
          node.children.map((child) => renderNode(child, depth + 1))}
      </div>
    );
  };

  return (
    // FIR-1772: inline on desktop, hidden under md (mobile uses the Sheet drawer).
    <div
      className={cn(
        "flex-col bg-muted/20",
        inSheet
          ? "flex h-full w-full"
          : "hidden w-56 shrink-0 border-r md:flex",
      )}
    >
      <div className="flex h-10 shrink-0 items-center justify-between border-b px-3">
        <span className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          <Layers className="h-3.5 w-3.5" />
          Folders
        </span>
        <button
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-accent"
          onClick={() => openCreate(null)}
          aria-label="New folder"
          title="New folder"
        >
          <FolderPlus className="h-4 w-4" />
        </button>
      </div>

      <ScrollArea className="flex-1">
        <div className="flex flex-col gap-0.5 p-2">
          <button
            type="button"
            className={cn(
              "flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm",
              selected === SELECT_ALL
                ? "bg-accent text-accent-foreground"
                : "hover:bg-accent/50",
            )}
            onClick={() => onSelect(SELECT_ALL)}
          >
            <Layers className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="flex-1 text-left">All {itemNoun}</span>
            <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
              {items.length}
            </span>
          </button>
          <button
            type="button"
            className={cn(
              "flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm",
              selected === SELECT_UNFILED
                ? "bg-accent text-accent-foreground"
                : "hover:bg-accent/50",
              dropTarget === SELECT_UNFILED &&
                "bg-primary/10 ring-1 ring-inset ring-primary/40",
            )}
            onClick={() => onSelect(SELECT_UNFILED)}
            {...dropHandlers(null, SELECT_UNFILED)}
          >
            <Inbox className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="flex-1 text-left">Unfiled</span>
            <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
              {unfiledCount}
            </span>
          </button>

          {tree.length > 0 && <div className="my-1 border-t" />}
          {tree.map((node) => renderNode(node, 0))}
        </div>
      </ScrollArea>

      {/* Create / rename dialog */}
      <Dialog
        open={nameDialog !== null}
        onOpenChange={(o) => !o && setNameDialog(null)}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>
              {nameDialog?.mode === "rename" ? "Rename folder" : "New folder"}
            </DialogTitle>
          </DialogHeader>
          <Input
            autoFocus
            value={nameDraft}
            onChange={(e) => setNameDraft(e.target.value)}
            placeholder="Folder name"
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                submitName();
              }
            }}
          />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setNameDialog(null)}>
              Cancel
            </Button>
            <Button size="sm" onClick={submitName} disabled={!nameDraft.trim()}>
              {nameDialog?.mode === "rename" ? "Save" : "Create"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirm */}
      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete folder?</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget
                ? `"${deleteTarget.name}" and its sub-folders will be removed. The ${itemNoun} inside move back to Unfiled — nothing is deleted.`
                : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (deleteTarget) {
                  deleteFolder.mutate(deleteTarget.id);
                  if (selected === deleteTarget.id) onSelect(SELECT_ALL);
                }
                setDeleteTarget(null);
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Add items dialog */}
      {addTarget && (
        <AddItemsDialog
          kind={kind}
          folder={addTarget}
          items={items}
          membership={membership}
          itemNoun={itemNoun}
          onClose={() => setAddTarget(null)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Add-items dialog: a searchable multi-select that files items into the folder
// (checking) or unfiles them from it (unchecking), applied on Save.
// ---------------------------------------------------------------------------

function AddItemsDialog({
  kind,
  folder,
  items,
  membership,
  itemNoun,
  onClose,
}: {
  kind: EntityFolderKind;
  folder: EntityFolder;
  items: EntityFolderViewItem[];
  membership: Map<string, string>;
  itemNoun: string;
  onClose: () => void;
}) {
  const setItem = useSetEntityFolderItem();
  const [search, setSearch] = useState("");
  const [checked, setChecked] = useState<Set<string>>(
    () =>
      new Set(
        items
          .filter((it) => membership.get(it.id) === folder.id)
          .map((it) => it.id),
      ),
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return items;
    return items.filter((it) => it.label.toLowerCase().includes(q));
  }, [items, search]);

  const save = () => {
    for (const it of items) {
      const wasHere = membership.get(it.id) === folder.id;
      const nowHere = checked.has(it.id);
      if (nowHere && !wasHere) {
        setItem.mutate({ kind, item_id: it.id, folder_id: folder.id });
      } else if (!nowHere && wasHere) {
        setItem.mutate({ kind, item_id: it.id, folder_id: null });
      }
    }
    onClose();
  };

  const toggle = (id: string) =>
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add {itemNoun} to "{folder.name}"</DialogTitle>
        </DialogHeader>
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={`Search ${itemNoun}…`}
          className="h-8"
        />
        <ScrollArea className="max-h-72">
          <div className="flex flex-col gap-0.5 pr-2">
            {filtered.length === 0 ? (
              <p className="px-2 py-6 text-center text-sm text-muted-foreground">
                No {itemNoun} found.
              </p>
            ) : (
              filtered.map((it) => {
                const elsewhere = membership.get(it.id);
                const inOther = elsewhere && elsewhere !== folder.id;
                return (
                  <label
                    key={it.id}
                    className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent/50"
                  >
                    <Checkbox
                      checked={checked.has(it.id)}
                      onCheckedChange={() => toggle(it.id)}
                    />
                    <span className="min-w-0 flex-1 truncate">{it.label}</span>
                    {inOther && (
                      <span className="shrink-0 text-xs text-muted-foreground/60">
                        in another folder
                      </span>
                    )}
                  </label>
                );
              })
            )}
          </div>
        </ScrollArea>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" onClick={save}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
