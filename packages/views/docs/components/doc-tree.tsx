"use client";
import {
  useMemo,
  useState,
  type DragEvent,
  type MouseEvent,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ChevronRight,
  FileText,
  Folder,
  FolderInput,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Collapsible, CollapsibleContent } from "@multica/ui/components/ui/collapsible";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@multica/ui/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  docTreeOptions,
  useCreateDoc,
  useCreateDocFolder,
  useMoveDoc,
  useMoveDocFolder,
  useRenameDocFolder,
  useUpdateDoc,
  useDeleteDoc,
  useDeleteDocFolder,
  removeDocDraft,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DocSummary, DocFolder } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import {
  PANEL_ROW_CLASS,
  PanelBody,
  PanelHeader,
  PanelList,
  panelRowAnchor,
  panelRowStateClass,
} from "../../layout/panel-nav";
import { TREE_ROW_BUTTON_CLASS } from "../../layout/panels/sidebar-tree";
import { shouldOpenRowMenu, useRowMenu } from "../../common/point-menu";
import { MoveDialog, type MoveTarget } from "./move-dialog";
import { useCanDeleteDoc } from "./use-can-delete-doc";
import { usePrefetchDoc, useWarmDocPage } from "./use-prefetch-doc";

type Item =
  | { kind: "doc"; item: DocSummary }
  | { kind: "folder"; item: DocFolder };

type RowMenu = ReturnType<typeof useRowMenu>;

const ROW_ACTION_CLASS = cn(
  TREE_ROW_BUTTON_CLASS,
  "text-muted-foreground data-popup-open:bg-surface-selected data-popup-open:text-foreground disabled:pointer-events-none",
);

/** A row's actions show on hover, keyboard focus or while their menu is open.
 *  At rest they take no space, so the title runs to the row's end. */
const ROW_ACTIONS_CLASS =
  "hidden shrink-0 items-center gap-0.5 group-hover/row:flex group-has-[:focus-visible]/row:flex group-data-menu-open/row:flex [@media(pointer:coarse)]:flex";

/** Marks the row a right-click menu is open on. */
const ROW_MENU_OPEN_CLASS =
  "data-menu-open:bg-nav-item-hover data-menu-open:text-nav-item-hover-foreground";

/** Folders a folder cannot move into: itself and everything beneath it. */
export function folderAndDescendants(
  folders: readonly Pick<DocFolder, "id" | "parentId">[],
  id: string,
): Set<string> {
  const blocked = new Set([id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const f of folders) {
      if (f.parentId && blocked.has(f.parentId) && !blocked.has(f.id)) {
        blocked.add(f.id);
        grew = true;
      }
    }
  }
  return blocked;
}

/** Where a dragged item may land: a folder id, or `null` for the top level. */
export function canDropInto(
  dragged: Item,
  destination: string | null,
  folders: readonly Pick<DocFolder, "id" | "parentId">[],
): boolean {
  if (dragged.kind === "doc") return dragged.item.folderId !== destination;
  if (dragged.item.parentId === destination) return false;
  return destination === null || !folderAndDescendants(folders, dragged.item.id).has(destination);
}
export function DeleteDocDialog({
  target,
  onOpenChange,
}: {
  target: Item | null;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { pathname, push } = useNavigation();
  const docDelete = useDeleteDoc();
  const folderDelete = useDeleteDocFolder();
  const pending = docDelete.isPending || folderDelete.isPending;
  const confirm = async () => {
    if (!target) return;
    try {
      if (target.kind === "doc") {
        await docDelete.mutateAsync(target.item.id);
        removeDocDraft(`${wsId}:${target.item.id}`);
      } else await folderDelete.mutateAsync(target.item.id);
      onOpenChange(false);
      if (
        pathname === p.docFolder(target.item.id) ||
        pathname.startsWith(p.docDetail(target.item.id))
      )
        push(p.docs());
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.errors.delete),
      );
    }
  };
  return (
    <AlertDialog
      open={!!target}
      onOpenChange={(open) => {
        if (!pending) onOpenChange(open);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {target?.kind === "folder"
              ? t(($) => $.delete.folder_title)
              : t(($) => $.delete.title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {target?.kind === "folder"
              ? t(($) => $.delete.folder_description, {
                  title: target?.item.title ?? "",
                })
              : t(($) => $.delete.description, {
                  title: target?.item.title ?? "",
                })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>
            {t(($) => $.actions.cancel)}
          </AlertDialogCancel>
          <Button
            variant="destructive"
            disabled={pending}
            onClick={() => void confirm()}
          >
            {t(($) => $.delete.confirm)}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function DocTree() {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { pathname, push } = useNavigation();
  const tree = useQuery(docTreeOptions(wsId));
  const prefetchDoc = usePrefetchDoc();
  useWarmDocPage(tree.data?.docs[0]?.id);
  const createDoc = useCreateDoc();
  const createFolder = useCreateDocFolder();
  const renameFolder = useRenameDocFolder();
  const updateDoc = useUpdateDoc();
  const moveDoc = useMoveDoc();
  const moveFolder = useMoveDocFolder();
  const [dragged, setDragged] = useState<Item | null>(null);
  // `undefined`: no drop target; `null`: the top level; otherwise a folder id.
  const [dropTarget, setDropTarget] = useState<string | null | undefined>(undefined);
  const folders = useMemo(() => tree.data?.folders ?? [], [tree.data]);
  const [form, setForm] = useState<{
    kind: "doc" | "folder";
    parentId: string | null;
    target?: Item;
  } | null>(null);
  const [name, setName] = useState("");
  const [moving, setMoving] = useState<MoveTarget | null>(null);
  const [deleting, setDeleting] = useState<Item | null>(null);
  const canDelete = useCanDeleteDoc();
  const pending =
    createDoc.isPending ||
    createFolder.isPending ||
    renameFolder.isPending ||
    updateDoc.isPending;
  // A new doc opens straight away, untitled; its page puts the caret in the
  // title, so naming it is just typing.
  const newDoc = async (folderId: string | null) => {
    try {
      const doc = await createDoc.mutateAsync({ title: "", folderId });
      push(p.docDetail(doc.id));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.errors.save),
      );
    }
  };
  const dragProps = (item: Item) => ({
    draggable: true,
    onDragStart: (event: DragEvent) => {
      event.stopPropagation();
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", item.item.title);
      setDragged(item);
    },
    onDragEnd: () => {
      setDragged(null);
      setDropTarget(undefined);
    },
  });
  // Every row is the drop target its position means: a folder row is that
  // folder, a doc row is the doc's folder, the blank tree is the top level.
  // The innermost target always answers and stops the event, even to refuse:
  // a refusal that bubbled would reach the top-level zone and move the item
  // to the root.
  const dropProps = (destination: string | null) => ({
    onDragOver: (event: DragEvent) => {
      if (!dragged) return;
      event.stopPropagation();
      if (!canDropInto(dragged, destination, folders)) {
        setDropTarget(undefined);
        return;
      }
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      setDropTarget(destination);
    },
    onDragLeave: (event: DragEvent) => {
      if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
      setDropTarget((current) => (current === destination ? undefined : current));
    },
    onDrop: (event: DragEvent) => {
      if (!dragged) return;
      event.preventDefault();
      event.stopPropagation();
      if (!canDropInto(dragged, destination, folders)) {
        setDragged(null);
        setDropTarget(undefined);
        return;
      }
      const item = dragged;
      setDragged(null);
      setDropTarget(undefined);
      const move =
        item.kind === "doc"
          ? moveDoc.mutateAsync({ id: item.item.id, folderId: destination })
          : moveFolder.mutateAsync({ id: item.item.id, parentId: destination });
      move.catch((error: unknown) => {
        toast.error(
          error instanceof Error ? error.message : t(($) => $.errors.move),
        );
      });
    },
  });
  const openForm = (
    kind: "doc" | "folder",
    parentId: string | null,
    target?: Item,
  ) => {
    setName(target?.item.title ?? "");
    setForm({ kind, parentId, target });
  };
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!form || !name.trim()) return;
    try {
      if (form.target?.kind === "doc")
        await updateDoc.mutateAsync({
          id: form.target.item.id,
          title: name.trim(),
          baseRevision: form.target.item.revision,
        });
      else if (form.target?.kind === "folder")
        await renameFolder.mutateAsync({
          id: form.target.item.id,
          title: name.trim(),
        });
      else
        await createFolder.mutateAsync({
          title: name.trim(),
          parentId: form.parentId,
        });
      setForm(null);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.errors.save),
      );
    }
  };
  // Right-click opens the same menu at the cursor.
  const menu = (target: Item, rowMenu: RowMenu) => (
    <DropdownMenu open={rowMenu.menu.open} onOpenChange={rowMenu.onOpenChange}>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={t(($) => $.tree.more, { title: target.item.title })}
            className={ROW_ACTION_CLASS}
          />
        }
      >
        <MoreHorizontal className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align={rowMenu.menu.point ? "start" : "end"}
        anchor={rowMenu.anchor}
      >
        {target.kind === "folder" && (
          <>
            <DropdownMenuItem onClick={() => void newDoc(target.item.id)}>
              <Plus />
              {t(($) => $.actions.new_doc)}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => openForm("folder", target.item.id)}
            >
              <Folder />
              {t(($) => $.actions.new_folder)}
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuItem onClick={() => openForm(target.kind, null, target)}>
          <Pencil />
          {t(($) => $.actions.rename)}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() =>
            setMoving({
              kind: target.kind,
              id: target.item.id,
              title: target.item.title,
            })
          }
        >
          <FolderInput />
          {t(($) => $.actions.move)}
        </DropdownMenuItem>
        {canDelete(target.item) && (
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleting(target)}
          >
            <Trash2 />
            {t(($) => $.actions.delete)}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
  // Nested rows hang off a connector on the folder icon's centre line
  // (`ml-5`), with their icons on the folder's label column: 1px border + 5px
  // + the row's 12px padding reaches the folder row's 12px + 16px icon + 10px
  // gap.
  const branch = (
    parent: string | null,
    ancestors: Set<string>,
  ): React.ReactNode => (
    <PanelList>
      {tree.data?.folders
        .filter((f) => f.parentId === parent && !ancestors.has(f.id))
        .map((folder) => (
          <DocFolderItem
            key={folder.id}
            folderId={folder.id}
            title={folder.title}
            isDropTarget={dropTarget === folder.id}
            dragProps={dragProps({ kind: "folder", item: folder })}
            dropProps={dropProps(folder.id)}
            actions={(rowMenu) => (
              <>
                <button
                  type="button"
                  aria-label={t(($) => $.tree.new_doc_in, { title: folder.title })}
                  title={t(($) => $.actions.new_doc)}
                  className={ROW_ACTION_CLASS}
                  disabled={createDoc.isPending}
                  onClick={() => void newDoc(folder.id)}
                >
                  <Plus className="size-3.5" />
                </button>
                {menu({ kind: "folder", item: folder }, rowMenu)}
              </>
            )}
          >
            {branch(folder.id, new Set([...ancestors, folder.id]))}
          </DocFolderItem>
        ))}
      {tree.data?.docs
        .filter((d) => d.folderId === parent)
        .map((doc) => {
          const active = pathname === p.docDetail(doc.id);
          return (
            <DocItem
              key={doc.id}
              href={p.docDetail(doc.id)}
              title={doc.title || t(($) => $.untitled)}
              active={active}
              dragProps={dragProps({ kind: "doc", item: doc })}
              dropProps={dropProps(doc.folderId)}
              linkProps={prefetchDoc(doc.id)}
              actions={(rowMenu) => menu({ kind: "doc", item: doc }, rowMenu)}
            />
          );
        })}
    </PanelList>
  );
  return (
    <>
      <PanelHeader
        title={
          <AppLink
            href={p.docs()}
            className="min-w-0 truncate rounded-md outline-none focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-ring"
          >
            {t(($) => $.kb)}
          </AppLink>
        }
      >
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <IconButton
                label={t(($) => $.actions.new)}
                tooltipSide="bottom"
                className="text-muted-foreground"
              >
                <Plus />
              </IconButton>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => void newDoc(null)}>
              {t(($) => $.actions.new_doc)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => openForm("folder", null)}>
              {t(($) => $.actions.new_folder)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </PanelHeader>
      <PanelBody>
        {/* The blank space under the tree is the top-level drop zone. */}
        <div
          {...dropProps(null)}
          className={cn(
            "min-h-full rounded-nav-item",
            dropTarget === null && "bg-nav-item-hover/50",
          )}
        >
          {tree.isError ? (
            <Button variant="ghost" onClick={() => void tree.refetch()}>
              {t(($) => $.editor.retry)}
            </Button>
          ) : tree.isPending ? (
            <p className="px-3 py-1.5 text-caption text-muted-foreground">
              {t(($) => $.history.loading)}
            </p>
          ) : (
            branch(null, new Set())
          )}
          {tree.data && !tree.data.docs.length && !tree.data.folders.length && (
            <p className="px-3 py-1.5 text-caption text-muted-foreground">
              {t(($) => $.tree.empty)}
            </p>
          )}
        </div>
      </PanelBody>
      <Dialog
        open={!!form}
        onOpenChange={(open) => {
          if (!open && !pending) setForm(null);
        }}
      >
        <DialogContent>
          <form onSubmit={submit}>
            <DialogHeader>
              <DialogTitle>
                {form?.target
                  ? t(($) => $.actions.rename)
                  : form?.kind === "folder"
                    ? t(($) => $.actions.new_folder)
                    : t(($) => $.actions.new_doc)}
              </DialogTitle>
            </DialogHeader>
            <label className="my-4 block text-body">
              {t(($) => $.editor.title_label)}
              <Input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={form?.kind === "folder" ? 200 : 300}
              />
            </label>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => setForm(null)}
              >
                {t(($) => $.actions.cancel)}
              </Button>
              <Button type="submit" disabled={pending || !name.trim()}>
                {form?.target
                  ? t(($) => $.actions.rename)
                  : t(($) => $.actions.new)}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <MoveDialog
        target={moving}
        onOpenChange={(open) => {
          if (!open) setMoving(null);
        }}
      />
      <DeleteDocDialog
        target={deleting}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
      />
    </>
  );
}

function openRowMenu(rowMenu: RowMenu) {
  return (event: MouseEvent) => {
    if (shouldOpenRowMenu(event)) rowMenu.openAtPointer(event);
  };
}

function DocItem({
  href,
  title,
  active,
  dragProps,
  dropProps,
  linkProps,
  actions,
}: {
  href: string;
  title: string;
  active: boolean;
  dragProps: Record<string, unknown>;
  dropProps: Record<string, unknown>;
  linkProps: Record<string, unknown>;
  actions: (rowMenu: RowMenu) => ReactNode;
}) {
  const rowMenu = useRowMenu();
  return (
    <li
      {...dragProps}
      {...dropProps}
      {...panelRowAnchor(active)}
      data-menu-open={rowMenu.menu.open ? "" : undefined}
      onContextMenu={openRowMenu(rowMenu)}
      className={cn(
        PANEL_ROW_CLASS,
        panelRowStateClass(active),
        !active && ROW_MENU_OPEN_CLASS,
        "group/row relative",
      )}
    >
      <FileText className="size-4 shrink-0" />
      <TruncatedTooltip content={title} side="right">
        {/* Stretched over the row, under its actions, so the whole
            row opens the doc. */}
        <AppLink
          href={href}
          aria-current={active ? "page" : undefined}
          {...linkProps}
          className="min-w-0 flex-1 truncate outline-none after:absolute after:inset-0 after:rounded-nav-item focus-visible:after:outline-2 focus-visible:after:outline-ring"
        >
          {title}
        </AppLink>
      </TruncatedTooltip>
      <span className={ROW_ACTIONS_CLASS}>{actions(rowMenu)}</span>
    </li>
  );
}

function DocFolderItem({
  folderId,
  title,
  isDropTarget,
  dragProps,
  dropProps,
  actions,
  children,
}: {
  folderId: string;
  title: string;
  isDropTarget: boolean;
  dragProps: Record<string, unknown>;
  dropProps: Record<string, unknown>;
  actions: (rowMenu: RowMenu) => ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(true);
  const rowMenu = useRowMenu();

  return (
    <Collapsible open={open} render={<li />}>
      <div
        data-doc-folder={folderId}
        {...dragProps}
        {...dropProps}
        data-menu-open={rowMenu.menu.open ? "" : undefined}
        onContextMenu={openRowMenu(rowMenu)}
        className={cn(
          PANEL_ROW_CLASS,
          panelRowStateClass(false),
          ROW_MENU_OPEN_CLASS,
          "group/row relative transition-[background-color,box-shadow] duration-150 motion-reduce:transition-none",
          isDropTarget && "bg-nav-item-hover ring-1 ring-ring",
        )}
      >
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-3 shrink-0 text-faint-foreground transition-transform duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none",
            open && "rotate-90",
          )}
        />
        <Folder className="size-4 shrink-0" />
        <TruncatedTooltip content={title} side="right">
          {/* Stretched over the row, under its actions, so the whole row
              toggles the folder while the actions stay sibling controls. */}
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className="min-w-0 flex-1 cursor-pointer truncate text-left outline-none after:absolute after:inset-0 after:rounded-nav-item focus-visible:after:outline-2 focus-visible:after:outline-ring"
          >
            {title}
          </button>
        </TruncatedTooltip>
        <span className={ROW_ACTIONS_CLASS}>{actions(rowMenu)}</span>
      </div>
      <CollapsibleContent>
        <div
          className="ml-5 border-l border-surface-border pl-[5px]"
          {...dropProps}
        >
          {children}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
