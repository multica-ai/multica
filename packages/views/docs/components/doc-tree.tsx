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
import { MoveDialog, type MoveTarget } from "./move-dialog";
import { useCanDeleteDoc } from "./use-can-delete-doc";
import { usePrefetchDoc, useWarmDocPage } from "./use-prefetch-doc";

type Item =
  | { kind: "doc"; item: DocSummary }
  | { kind: "folder"; item: DocFolder };

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
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={t(($) => $.tree.more, { title: target.item.title })}
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
  const branch = (
    parent: string | null,
    ancestors: Set<string>,
  ): React.ReactNode => (
    <ul className="space-y-0.5">
      {tree.data?.folders
        .filter((f) => f.parentId === parent && !ancestors.has(f.id))
        .map((folder) => (
          <li key={folder.id}>
            <details open>
              <summary
                {...dragProps({ kind: "folder", item: folder })}
                {...dropProps(folder.id)}
                className={`group flex cursor-pointer items-center gap-1 rounded-md px-1 py-1 text-body hover:bg-surface-hover ${dropTarget === folder.id ? "bg-surface-hover ring-1 ring-ring" : ""}`}
              >
                <Folder className="size-4 shrink-0 text-muted-foreground" />
                <TruncatedTooltip content={folder.title} side="right">
                  <span className="min-w-0 flex-1 truncate">{folder.title}</span>
                </TruncatedTooltip>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={t(($) => $.tree.new_doc_in, { title: folder.title })}
                  title={t(($) => $.actions.new_doc)}
                  className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                  disabled={createDoc.isPending}
                  onClick={(event) => {
                    // Inside <summary>: a click would also fold the folder.
                    event.preventDefault();
                    void newDoc(folder.id);
                  }}
                >
                  <Plus className="size-3.5" />
                </Button>
                {menu({ kind: "folder", item: folder })}
              </summary>
              <div className="ml-3 border-l pl-2" {...dropProps(folder.id)}>
                {branch(folder.id, new Set([...ancestors, folder.id]))}
              </div>
            </details>
          </li>
        ))}
      {tree.data?.docs
        .filter((d) => d.folderId === parent)
        .map((doc) => (
          <li
            key={doc.id}
            {...dragProps({ kind: "doc", item: doc })}
            {...dropProps(doc.folderId)}
            className={`flex items-center gap-1 rounded-md px-1 py-1 text-body ${pathname === p.docDetail(doc.id) ? "bg-surface-selected text-surface-selected-foreground" : "hover:bg-surface-hover"}`}
          >
            <FileText className="size-4 shrink-0 text-muted-foreground" />
            <TruncatedTooltip content={doc.title || t(($) => $.untitled)} side="right">
              <AppLink
                href={p.docDetail(doc.id)}
                {...prefetchDoc(doc.id)}
                className="min-w-0 flex-1 truncate"
              >
                {doc.title || t(($) => $.untitled)}
              </AppLink>
            </TruncatedTooltip>
            {menu({ kind: "doc", item: doc })}
          </li>
        ))}
    </ul>
  );
  return (
    <>
      <div className="flex h-12 shrink-0 items-center justify-between px-3">
        <AppLink href={p.docs()} className="text-body font-medium">
          {t(($) => $.kb)}
        </AppLink>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t(($) => $.actions.new)}
              />
            }
          >
            <Plus className="size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem onClick={() => void newDoc(null)}>
              {t(($) => $.actions.new_doc)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => openForm("folder", null)}>
              {t(($) => $.actions.new_folder)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div
        {...dropProps(null)}
        className={`min-h-0 flex-1 overflow-auto px-2 pb-3 ${dropTarget === null ? "bg-surface-hover/50" : ""}`}
      >
        {tree.isError ? (
          <Button variant="ghost" onClick={() => void tree.refetch()}>
            {t(($) => $.editor.retry)}
          </Button>
        ) : tree.isPending ? (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.history.loading)}
          </span>
        ) : (
          branch(null, new Set())
        )}
        {tree.data && !tree.data.docs.length && !tree.data.folders.length && (
          <p className="p-2 text-caption text-muted-foreground">
            {t(($) => $.tree.empty)}
          </p>
        )}
      </div>
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
