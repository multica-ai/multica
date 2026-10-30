"use client";

import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  FileCode,
  FileText,
  Folder,
  FolderInput,
  FolderOpen,
  FolderPlus,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import {
  buildDocTree,
  canMoveFolder,
  docTreeOptions,
  folderAncestry,
  useDeleteDoc,
  useDeleteDocFolder,
  useMoveDoc,
  useMoveDocFolder,
  useRenameDocFolder,
  type DocTreeNode,
} from "@multica/core/docs";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DocFolder, DocSummary } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { PANEL_ROW_CLASS, panelRowStateClass } from "../../layout/panel-nav";
import { TREE_ROW_BUTTON_CLASS, TreeRow } from "../../layout/panels/sidebar-tree";
import { MoveDialog, type MoveTarget } from "./move-dialog";
import { useDocCommands } from "./use-doc-commands";

const ROW_ACTION_CLASS = cn(TREE_ROW_BUTTON_CLASS, "text-muted-foreground");
// Out of the layout until the row is hovered or focused, so labels get the full width.
const ROW_ACTIONS_CLASS =
  "hidden shrink-0 items-center group-focus-within/row:flex group-hover/row:flex has-[[data-popup-open]]:flex [@media(pointer:coarse)]:flex";
const NESTED_LIST_CLASS = "mt-px ml-5 space-y-px border-l border-surface-border pl-1.5";
const DRAG_TYPE = "application/x-multica-doc-node";

type DragItem = { kind: "doc" | "folder"; id: string };

/** Which doc or folder the route has open, read from `/docs/...`. */
export function docRouteTarget(pathname: string, docsPath: string): { docId: string | null; folderId: string | null } {
  if (!pathname.startsWith(docsPath + "/")) return { docId: null, folderId: null };
  const [first, second] = pathname.slice(docsPath.length + 1).split("/");
  if (first === "folders") return { docId: null, folderId: second ? decodeURIComponent(second) : null };
  return { docId: first ? decodeURIComponent(first) : null, folderId: null };
}

function readDrag(event: React.DragEvent): DragItem | null {
  try {
    const raw = event.dataTransfer.getData(DRAG_TYPE);
    const item = raw ? (JSON.parse(raw) as DragItem) : null;
    return item && (item.kind === "doc" || item.kind === "folder") && typeof item.id === "string" ? item : null;
  } catch {
    return null;
  }
}

function NameInput({
  initial,
  placeholder,
  depthClass,
  onSubmit,
  onCancel,
}: {
  initial: string;
  placeholder: string;
  depthClass?: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(initial);
  const done = useRef(false);
  const finish = (commit: boolean) => {
    if (done.current) return;
    done.current = true;
    const title = value.trim();
    if (commit && title) onSubmit(title);
    else onCancel();
  };
  return (
    <div className={cn(PANEL_ROW_CLASS, "gap-2.5 bg-surface-hover", depthClass)}>
      <Folder className="size-4 shrink-0 text-muted-foreground" />
      <input
        autoFocus
        value={value}
        maxLength={200}
        placeholder={placeholder}
        aria-label={placeholder}
        onFocus={(event) => event.currentTarget.select()}
        onChange={(event) => setValue(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.nativeEvent.isComposing) finish(true);
          else if (event.key === "Escape") finish(false);
        }}
        onBlur={() => finish(true)}
        className="min-w-0 flex-1 bg-transparent text-body text-foreground outline-none placeholder:text-faint-foreground"
      />
    </div>
  );
}

/** Confirms deleting a doc; leaves the doc's page if it is open. */
export function DeleteDocDialog({
  target,
  onOpenChange,
}: {
  target: { kind: "doc"; doc: DocSummary } | null;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const { pathname, push } = useNavigation();
  const deleteDoc = useDeleteDoc();
  const doc = target?.doc;
  const confirm = () => {
    if (!doc) return;
    deleteDoc.mutate(doc.id, {
      onSuccess: () => {
        onOpenChange(false);
        if (pathname.startsWith(p.docDetail(doc.id))) push(p.docs());
      },
      onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.delete)),
    });
  };
  return (
    <Dialog open={!!doc} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.delete.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.delete.description, { title: doc?.title ?? "" })}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.actions.cancel)}
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={deleteDoc.isPending}>
            {t(($) => $.delete.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface TreeState {
  activeDocId: string | null;
  activeFolderId: string | null;
  expanded: Set<string>;
  setExpanded: (id: string, open: boolean) => void;
  naming: { parentId: string | null } | null;
  setNaming: (naming: { parentId: string | null } | null) => void;
  renaming: string | null;
  setRenaming: (id: string | null) => void;
  dropTarget: string | null;
  setDropTarget: (id: string | null) => void;
  onDrop: (item: DragItem, folderId: string | null) => void;
  onMove: (target: MoveTarget) => void;
  onDelete: (doc: DocSummary) => void;
}

function DocRow({ doc, state }: { doc: DocSummary; state: TreeState }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  return (
    <li>
      <TreeRow
        href={p.docDetail(doc.id)}
        label={doc.title || t(($) => $.untitled)}
        active={state.activeDocId === doc.id}
        icon={doc.format === "html" ? <FileCode className="size-4 shrink-0" /> : <FileText className="size-4 shrink-0" />}
        draggable
        onDragStart={(event) => {
          event.dataTransfer.setData(DRAG_TYPE, JSON.stringify({ kind: "doc", id: doc.id }));
          event.dataTransfer.effectAllowed = "move";
        }}
        trailing={
          <>
            {doc.unread && (
              <span aria-label={t(($) => $.tree.unread)} className="size-1.5 shrink-0 rounded-full bg-brand group-hover/row:hidden" />
            )}
            <span className={ROW_ACTIONS_CLASS}>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      aria-label={t(($) => $.tree.more, { title: doc.title })}
                      onPointerDown={(event) => event.stopPropagation()}
                      className={ROW_ACTION_CLASS}
                    />
                  }
                >
                  <MoreHorizontal className="size-3.5" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="right" className="w-auto min-w-44">
                  <DropdownMenuItem onClick={() => state.onMove({ kind: "doc", id: doc.id, title: doc.title })}>
                    <FolderInput />
                    {t(($) => $.actions.move)}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => state.onDelete(doc)}>
                    <Trash2 />
                    {t(($) => $.actions.delete)}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </span>
          </>
        }
      />
    </li>
  );
}

function FolderRow({
  folder,
  childNodes,
  unread,
  state,
}: {
  folder: DocFolder;
  childNodes: DocTreeNode[];
  unread: number;
  state: TreeState;
}) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const listId = useId();
  const { newDoc, newFolder } = useDocCommands();
  const renameFolder = useRenameDocFolder();
  const deleteFolder = useDeleteDocFolder();
  const expanded = state.expanded.has(folder.id);
  const empty = childNodes.length === 0;
  const naming = state.naming?.parentId === folder.id;

  const expand = () => state.setExpanded(folder.id, true);

  if (state.renaming === folder.id) {
    return (
      <li>
        <NameInput
          initial={folder.title}
          placeholder={t(($) => $.tree.folder_name)}
          onCancel={() => state.setRenaming(null)}
          onSubmit={(title) => {
            state.setRenaming(null);
            if (title !== folder.title) {
              renameFolder.mutate(
                { id: folder.id, title },
                { onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.rename)) },
              );
            }
          }}
        />
      </li>
    );
  }

  return (
    <li>
      <TreeRow
        href={p.docFolder(folder.id)}
        label={folder.title}
        active={state.activeFolderId === folder.id}
        icon={expanded ? <FolderOpen className="size-4 shrink-0" /> : <Folder className="size-4 shrink-0" />}
        expanded={expanded}
        onExpandedChange={(open) => state.setExpanded(folder.id, open)}
        listId={listId}
        toggleLabel={t(($) => $.tree.toggle, { title: folder.title })}
        draggable
        onDragStart={(event) => {
          event.dataTransfer.setData(DRAG_TYPE, JSON.stringify({ kind: "folder", id: folder.id }));
          event.dataTransfer.effectAllowed = "move";
        }}
        onDragOver={(event) => {
          if (!event.dataTransfer.types.includes(DRAG_TYPE)) return;
          event.preventDefault();
          event.stopPropagation();
          state.setDropTarget(folder.id);
        }}
        onDragLeave={() => state.setDropTarget(null)}
        onDrop={(event) => {
          event.preventDefault();
          event.stopPropagation();
          state.setDropTarget(null);
          const item = readDrag(event);
          if (item) {
            state.onDrop(item, folder.id);
            expand();
          }
        }}
        className={cn(state.dropTarget === folder.id && "bg-brand/10 ring-1 ring-brand/40")}
        trailing={
          <>
            {!expanded && unread > 0 && (
              <span aria-label={t(($) => $.tree.unread)} className="size-1.5 shrink-0 rounded-full bg-brand group-hover/row:hidden" />
            )}
            <span className={ROW_ACTIONS_CLASS}>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      aria-label={t(($) => $.tree.add_in, { title: folder.title })}
                      onPointerDown={(event) => event.stopPropagation()}
                      className={ROW_ACTION_CLASS}
                    />
                  }
                >
                  <Plus className="size-3.5" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="right" className="w-auto min-w-44">
                  <DropdownMenuItem onClick={() => newDoc("markdown", folder.id)}>
                    <FileText />
                    {t(($) => $.actions.new_doc)}
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => newDoc("html", folder.id)}>
                    <FileCode />
                    {t(($) => $.actions.new_html_doc)}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => {
                      expand();
                      state.setNaming({ parentId: folder.id });
                    }}
                  >
                    <FolderPlus />
                    {t(($) => $.actions.new_folder)}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      aria-label={t(($) => $.tree.more, { title: folder.title })}
                      onPointerDown={(event) => event.stopPropagation()}
                      className={ROW_ACTION_CLASS}
                    />
                  }
                >
                  <MoreHorizontal className="size-3.5" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="right" className="w-auto min-w-44">
                  <DropdownMenuItem onClick={() => state.setRenaming(folder.id)}>
                    <Pencil />
                    {t(($) => $.actions.rename)}
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => state.onMove({ kind: "folder", id: folder.id, title: folder.title })}>
                    <FolderInput />
                    {t(($) => $.actions.move)}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    disabled={!empty}
                    title={empty ? undefined : t(($) => $.tree.delete_folder_not_empty)}
                    onClick={() =>
                      deleteFolder.mutate(folder.id, {
                        onError: (error) =>
                          toast.error(error instanceof Error ? error.message : t(($) => $.errors.delete)),
                      })
                    }
                  >
                    <Trash2 />
                    {empty ? t(($) => $.actions.delete) : t(($) => $.tree.delete_folder_not_empty)}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </span>
          </>
        }
      />
      {expanded && (
        <ul id={listId} className={NESTED_LIST_CLASS}>
          {naming && (
            <li>
              <NameInput
                initial=""
                placeholder={t(($) => $.tree.folder_name)}
                onCancel={() => state.setNaming(null)}
                onSubmit={(title) => {
                  state.setNaming(null);
                  newFolder(title, folder.id);
                }}
              />
            </li>
          )}
          {childNodes.length === 0 && !naming ? (
            <li className="flex h-8 items-center px-3 text-caption text-faint-foreground">{t(($) => $.tree.empty_folder)}</li>
          ) : (
            <TreeNodes nodes={childNodes} state={state} />
          )}
        </ul>
      )}
    </li>
  );
}

function TreeNodes({ nodes, state }: { nodes: DocTreeNode[]; state: TreeState }) {
  return (
    <>
      {nodes.map((node) =>
        node.kind === "folder" ? (
          <FolderRow
            key={node.folder.id}
            folder={node.folder}
            childNodes={node.children}
            unread={node.unread}
            state={state}
          />
        ) : (
          <DocRow key={node.doc.id} doc={node.doc} state={state} />
        ),
      )}
    </>
  );
}

/**
 * The knowledge base tree in the panel: folders nest to any depth, and docs
 * and folders move by dragging onto a folder (or the section, for the root)
 * or through "Move to".
 */
export function DocTree({
  wsId,
  naming,
  onNamingChange,
}: {
  wsId: string;
  /** A folder being named at the root (`parentId: null`) or inside one. */
  naming: { parentId: string | null } | null;
  onNamingChange: (naming: { parentId: string | null } | null) => void;
}) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const { data: tree, isPending } = useQuery({ ...docTreeOptions(wsId), enabled: !!wsId });
  const nodes = useMemo(() => (tree ? buildDocTree(tree) : []), [tree]);
  const { newFolder } = useDocCommands();
  const moveDoc = useMoveDoc();
  const moveFolder = useMoveDocFolder();

  const [expanded, setExpandedSet] = useState<Set<string>>(() => new Set());
  const [renaming, setRenaming] = useState<string | null>(null);
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const [moveTarget, setMoveTarget] = useState<MoveTarget | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DocSummary | null>(null);

  const { docId: activeDocId, folderId: activeFolderId } = docRouteTarget(pathname, p.docs());
  const activeParent = activeDocId
    ? (tree?.docs.find((d) => d.id === activeDocId)?.folder_id ?? null)
    : activeFolderId
      ? (tree?.folders.find((f) => f.id === activeFolderId)?.parent_id ?? null)
      : null;

  // Reveal what the route has open.
  const revealKey = tree && activeParent ? folderAncestry(tree.folders, activeParent).map((f) => f.id).join("/") : "";
  useEffect(() => {
    if (!revealKey) return;
    setExpandedSet((prev) => {
      const ids = revealKey.split("/");
      if (ids.every((id) => prev.has(id))) return prev;
      const next = new Set(prev);
      for (const id of ids) next.add(id);
      return next;
    });
  }, [revealKey]);

  const setExpanded = (id: string, open: boolean) =>
    setExpandedSet((prev) => {
      if (prev.has(id) === open) return prev;
      const next = new Set(prev);
      if (open) next.add(id);
      else next.delete(id);
      return next;
    });

  const onDrop = (item: DragItem, folderId: string | null) => {
    if (!tree) return;
    const onError = (error: unknown) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.move));
    if (item.kind === "doc") {
      const doc = tree.docs.find((d) => d.id === item.id);
      if (doc && (doc.folder_id ?? null) !== folderId) moveDoc.mutate({ id: item.id, folderId }, { onError });
      return;
    }
    const folder = tree.folders.find((f) => f.id === item.id);
    if (!folder || (folder.parent_id ?? null) === folderId || folder.id === folderId) return;
    if (!canMoveFolder(tree.folders, folder.id, folderId)) {
      toast.error(t(($) => $.errors.move_into_self));
      return;
    }
    moveFolder.mutate({ id: item.id, parentId: folderId }, { onError });
  };

  const state: TreeState = {
    activeDocId,
    activeFolderId,
    expanded,
    setExpanded,
    naming,
    setNaming: onNamingChange,
    renaming,
    setRenaming,
    dropTarget,
    setDropTarget,
    onDrop,
    onMove: setMoveTarget,
    onDelete: setDeleteTarget,
  };

  const rootDrop = dropTarget === "__root__";

  return (
    <div
      onDragOver={(event) => {
        if (!event.dataTransfer.types.includes(DRAG_TYPE)) return;
        event.preventDefault();
        setDropTarget("__root__");
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTarget(null);
      }}
      onDrop={(event) => {
        event.preventDefault();
        setDropTarget(null);
        const item = readDrag(event);
        if (item) onDrop(item, null);
      }}
      className={cn("rounded-lg", rootDrop && "bg-brand/5 ring-1 ring-brand/30")}
    >
      <ul className="space-y-px">
        {naming?.parentId === null && (
          <li>
            <NameInput
              initial=""
              placeholder={t(($) => $.tree.folder_name)}
              onCancel={() => onNamingChange(null)}
              onSubmit={(title) => {
                onNamingChange(null);
                newFolder(title, null);
              }}
            />
          </li>
        )}
        {isPending ? (
          <li className={cn(PANEL_ROW_CLASS, panelRowStateClass(false))}>
            <div className="size-4 shrink-0 rounded-xs bg-muted" />
            <div className="h-3 w-28 rounded-xs bg-muted" />
          </li>
        ) : nodes.length === 0 && !naming ? (
          <li className="px-3 py-1.5 text-caption text-faint-foreground">{t(($) => $.tree.empty)}</li>
        ) : (
          <TreeNodes nodes={nodes} state={state} />
        )}
      </ul>
      <MoveDialog target={moveTarget} onOpenChange={(open) => !open && setMoveTarget(null)} />
      <DeleteDocDialog
        target={deleteTarget ? { kind: "doc", doc: deleteTarget } : null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      />
    </div>
  );
}
