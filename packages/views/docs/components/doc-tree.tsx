"use client";
import { useMemo, useState, type DragEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  FileText,
  Folder,
  FolderInput,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
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
import { MoveDialog, type MoveTarget } from "./move-dialog";
import { useCanDeleteDoc } from "./use-can-delete-doc";
import { usePrefetchDoc, useWarmDocPage } from "./use-prefetch-doc";

type Item =
  | { kind: "doc"; item: DocSummary }
  | { kind: "folder"; item: DocFolder };

/** A row's actions show on hover or focus, as on the other panels' rows. */
const ROW_ACTION_CLASS = cn(
  TREE_ROW_BUTTON_CLASS,
  "text-muted-foreground opacity-0 group-hover/row:opacity-100 data-popup-open:bg-surface-selected data-popup-open:text-foreground data-popup-open:opacity-100 disabled:pointer-events-none [@media(pointer:coarse)]:opacity-100",
);

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
  const menu = (target: Item) => (
    <DropdownMenu>
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
      <DropdownMenuContent align="end">
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
          <li key={folder.id}>
            <details open>
              <summary
                {...dragProps({ kind: "folder", item: folder })}
                {...dropProps(folder.id)}
                className={cn(
                  PANEL_ROW_CLASS,
                  panelRowStateClass(false),
                  "group/row cursor-pointer",
                  dropTarget === folder.id && "bg-nav-item-hover ring-1 ring-ring",
                )}
              >
                <Folder className="size-4 shrink-0" />
                <TruncatedTooltip content={folder.title} side="right">
                  <span className="min-w-0 flex-1 truncate">{folder.title}</span>
                </TruncatedTooltip>
                <button
                  type="button"
                  aria-label={t(($) => $.tree.new_doc_in, { title: folder.title })}
                  title={t(($) => $.actions.new_doc)}
                  className={ROW_ACTION_CLASS}
                  disabled={createDoc.isPending}
                  onClick={(event) => {
                    // Inside <summary>: a click would also fold the folder.
                    event.preventDefault();
                    void newDoc(folder.id);
                  }}
                >
                  <Plus className="size-3.5" />
                </button>
                {menu({ kind: "folder", item: folder })}
              </summary>
              <div
                className="ml-5 border-l border-surface-border pl-[5px]"
                {...dropProps(folder.id)}
              >
                {branch(folder.id, new Set([...ancestors, folder.id]))}
              </div>
            </details>
          </li>
        ))}
      {tree.data?.docs
        .filter((d) => d.folderId === parent)
        .map((doc) => {
          const active = pathname === p.docDetail(doc.id);
          const title = doc.title || t(($) => $.untitled);
          return (
            <li
              key={doc.id}
              {...dragProps({ kind: "doc", item: doc })}
              {...dropProps(doc.folderId)}
              {...panelRowAnchor(active)}
              className={cn(
                PANEL_ROW_CLASS,
                panelRowStateClass(active),
                "group/row relative",
              )}
            >
              <FileText className="size-4 shrink-0" />
              <TruncatedTooltip content={title} side="right">
                {/* Stretched over the row, under its actions, so the whole
                    row opens the doc. */}
                <AppLink
                  href={p.docDetail(doc.id)}
                  aria-current={active ? "page" : undefined}
                  {...prefetchDoc(doc.id)}
                  className="min-w-0 flex-1 truncate outline-none after:absolute after:inset-0 after:rounded-nav-item focus-visible:after:outline-2 focus-visible:after:outline-ring"
                >
                  {title}
                </AppLink>
              </TruncatedTooltip>
              {menu({ kind: "doc", item: doc })}
            </li>
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
            className="min-w-0 truncate rounded-md outline-none focus-visible:outline-2 focus-visible:outline-ring"
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
